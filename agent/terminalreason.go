package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Autumn-27/norma/harness"
)

// runTrace retains the latest tool call so an interrupted run can identify the
// operation that was still in flight.
type runTrace struct {
	startedAt time.Time
	id        string
	name      string
	input     string
	at        time.Time
	pending   bool
}

func (t *runTrace) start(id, name, input string) {
	t.id, t.name, t.input, t.at, t.pending = id, name, input, time.Now(), true
}

func (t *runTrace) done(id string) {
	if id == t.id {
		t.pending = false
	}
}

var reasonHint = map[harness.TerminalReason]string{
	harness.ReasonCompleted:         "모델이 이번 턴을 정상적으로 끝냈지만 텍스트 요약을 남기지 않았습니다. 사실과 자산은 이번 도구 호출 기록을 기준으로 합니다",
	harness.ReasonMaxTurns:          "단계 수 한도(MaxTurns)에 도달했습니다. SDK가 마무리를 수행하여 사실과 자산을 기록했으며 의도는 실패가 아니라 exhausted로 표시하여 계획자가 다른 방향으로 계속 진행하도록 합니다",
	harness.ReasonTimeout:           "단일 실행의 경과 시간 예산(MaxDuration)에 도달했습니다. 실행 중인 도구를 중단하고 즉시 마무리 단계에 들어가 확인한 사실과 자산을 기록하며 의도는 exhausted로 표시됩니다",
	harness.ReasonModelError:        "모델 또는 API 호출 실패(네트워크, 인증, 요청 제한, 공급자 5xx 등)입니다. 재시도를 소진하면 의도를 blocked로 표시합니다. 전송 계층 장애로 이 의도를 사실상 제대로 탐색하지 못했으므로 실행 과정(get_worker_trace)을 확인한 뒤 재배정 또는 방법 변경을 결정하세요",
	harness.ReasonBlockingLimit:     "컨텍스트 길이가 강제 상한에 도달하여 요청 전송 전에 차단되었습니다. 의도 범위를 좁히거나 도구 반환 내용을 압축해야 합니다",
	harness.ReasonPromptTooLong:     "프롬프트가 너무 길고 컨텍스트 압축 재시도도 모두 소진하여 더 이상 실행할 수 없습니다",
	harness.ReasonImageError:        "현재 모델은 이번 멀티모달 콘텐츠를 지원하지 않습니다. 시각 입력을 지원하는 모델로 바꾸거나 도구가 이미지를 반환하지 않게 하세요",
	harness.ReasonStopHookPrevented: "Stop 훅이 이번 턴 종료를 막았고 이후 계속 진행하지 못했습니다. 작업 Guard 규칙이 지나치게 엄격한지 확인하세요",
	harness.ReasonHookStopped:       "도구 또는 훅이 범위 밖 대상이나 비활성 명령 등으로 인해 실행을 명시적으로 중지했습니다. 마지막 tool_result의 차단 설명을 확인하세요",
	harness.ReasonAbortedStreaming:  "모델 출력 스트리밍 생성 단계에서 실행이 취소되었습니다",
	harness.ReasonAbortedTools:      "도구 실행 단계에서 실행이 취소되었습니다",
}

// terminalText renders a terminal event with no final text into a compact summary
// and a Markdown detail block.
func terminalText(ctx context.Context, term *harness.Terminal, tr *runTrace) (string, string) {
	reason := term.Reason
	aborted := reason == harness.ReasonAbortedStreaming || reason == harness.ReasonAbortedTools
	// Prompt may return ctx.Err directly without a terminal event. Preserve the
	// cancellation cause instead of falling back to an empty/unknown terminal reason.
	if reason == "" && ctx.Err() != nil {
		aborted = true
	}

	var sum string
	if aborted {
		_, short, _, ok := AbortReason(ctx)
		if !ok {
			short = "취소 이유를 가져오지 못했습니다"
		}
		stage := "실행 과정"
		switch reason {
		case harness.ReasonAbortedStreaming:
			stage = "모델 출력 단계"
		case harness.ReasonAbortedTools:
			stage = "도구 실행 단계"
		}
		sum = "(실행 중단: " + short + "; 중단 위치: " + stage + progressSuffix(term, tr) + ", 미완료)"
	} else if reason == harness.ReasonMaxTurns || reason == harness.ReasonTimeout {
		sum = "(실행 예산 상한에 도달(" + string(reason) + "), 마무리하여 사실을 기록함" + progressSuffix(term, tr) + "; 이번에는 텍스트 요약 없음)"
	} else {
		hint := terminalReasonHint(reason)
		sum = "(텍스트 요약 없음, 최종 상태 " + terminalReasonLabel(reason) + "：" + firstLine(hint, 80) + "）"
	}

	var b strings.Builder
	b.WriteString(sum)
	b.WriteString("\n\n")
	displayReason := terminalReasonLabel(reason)
	fmt.Fprintf(&b, "- **최종 상태**: `%s` - %s\n", displayReason, terminalReasonHint(reason))
	if aborted {
		code, _, why, ok := AbortReason(ctx)
		if ok {
			fmt.Fprintf(&b, "- **중단 이유** (`%s`): %s\n", code, why)
		} else {
			b.WriteString("- **중단 이유**: 가져올 수 없습니다. 취소한 측에서 context.WithCancelCause로 명명된 사유를 첨부하지 않았을 수 있습니다\n")
		}
	}
	if term.Err != nil {
		fmt.Fprintf(&b, "- **하위 오류**: `%v`\n", term.Err)
	}
	if aborted && strings.TrimSpace(term.Text) != "" {
		b.WriteString("- **취소 전에 생성된 일부 출력**:\n\n")
		b.WriteString(term.Text)
		b.WriteString("\n\n")
	}
	if term.Turns > 0 {
		fmt.Fprintf(&b, "- **실행한 턴**: 모델 턴 %d회\n", term.Turns)
	}
	if !tr.startedAt.IsZero() {
		fmt.Fprintf(&b, "- **이번 실행 시간**: %s\n", roundDur(time.Since(tr.startedAt)))
	}
	if u := term.Usage; u.InputTokens+u.OutputTokens+u.CacheReadTokens+u.CacheWriteTokens > 0 {
		fmt.Fprintf(&b, "- **누적 토큰**: 입력 %d / 출력 %d / 캐시 읽기 %d / 캐시 쓰기 %d\n",
			u.InputTokens, u.OutputTokens, u.CacheReadTokens, u.CacheWriteTokens)
	}
	if tr.name == "" {
		b.WriteString("- **도구 호출**: 이번 실행은 도구를 호출하기 전에 종료되었습니다\n")
	} else if tr.pending {
		fmt.Fprintf(&b, "- **중단 시 실행 중인 도구**: `%s`(실행 시간 %s, **결과가 반환되지 않음**)\n\n  ```json\n  %s\n  ```\n",
			tr.name, roundDur(time.Since(tr.at)), firstLine(tr.input, 300))
	} else {
		fmt.Fprintf(&b, "- **중단 전 마지막 도구**: `%s`(정상 반환됨)\n", tr.name)
	}
	return sum, b.String()
}

func terminalReasonLabel(reason harness.TerminalReason) string {
	if reason == "" {
		return "context_canceled"
	}
	return string(reason)
}

func terminalReasonHint(reason harness.TerminalReason) string {
	if hint := reasonHint[reason]; hint != "" {
		return hint
	}
	if reason == "" {
		return "실행 context가 취소되었지만 하위 계층에서 Terminal 이벤트를 생성하지 않았습니다"
	}
	return "알 수 없는 최종 상태입니다. harness에 TerminalReason이 추가되었을 수 있으므로 reasonHint를 보완하세요"
}

func progressSuffix(term *harness.Terminal, tr *runTrace) string {
	var parts []string
	if term.Turns > 0 {
		parts = append(parts, fmt.Sprintf("%d회", term.Turns))
	}
	if !tr.startedAt.IsZero() {
		parts = append(parts, roundDur(time.Since(tr.startedAt)))
	}
	if len(parts) == 0 {
		return ""
	}
	return ", 실행 시간 " + strings.Join(parts, " / ")
}

func roundDur(d time.Duration) string {
	switch {
	case d < time.Minute:
		return d.Round(100 * time.Millisecond).String()
	case d < time.Hour:
		return d.Round(time.Second).String()
	default:
		return d.Round(time.Minute).String()
	}
}
