package agent

import (
	"context"
	"errors"
	"fmt"
)

// AbortCause names why an agent run's context was cancelled. Every cancellation
// site should attach one so the activity trace can report the real initiator.
type AbortCause struct {
	Code  string
	Short string
	Text  string
}

func (c *AbortCause) Error() string { return c.Text }

func cause(code, short, text string) *AbortCause {
	return &AbortCause{Code: code, Short: short, Text: text}
}

// Causef builds a cause that includes runtime-specific detail.
func Causef(code, short, format string, args ...any) *AbortCause {
	return &AbortCause{Code: code, Short: short, Text: fmt.Sprintf(format, args...)}
}

var (
	// Task-level execution context.
	AbortPausedByUser = cause("paused_by_user", "사용자가 작업을 일시 중지했습니다",
		"사용자가 작업 제어 API(POST /api/tasks/{id}/control, action=pause)로 작업을 일시 중지했습니다. 이번 Planner/Worker 실행을 명시적으로 취소했으며 실행 중인 의도는 frontier(open)로 돌아갑니다. 작업을 재개하면 다시 할당받아 처음부터 실행합니다")
	AbortPausedByOrchestrator = cause("paused_by_orchestrator", "조정 에이전트가 작업을 일시 중지했습니다",
		"조정 에이전트가 pause_task 도구로 이 작업을 일시 중지했습니다. 이번 Planner/Worker 실행을 명시적으로 취소했으며 실행 중인 의도는 frontier(open)로 돌아가 재개 후 다시 실행합니다")
	AbortTaskDeleted = cause("task_deleted", "작업이 삭제되었습니다",
		"작업을 삭제하는 중입니다(DELETE /api/tasks/{id}). 삭제 동기화 장치가 해당 작업에서 실행 중인 Planner, Worker 및 주 에이전트를 취소했으며 이번 실행 결과는 더 이상 사용하지 않습니다")
	AbortPausedOnReload = cause("paused_on_reload", "백엔드가 작업의 일시 중지 상태를 복원했습니다",
		"백엔드가 시작할 때 데이터베이스에 저장된 상태에 따라 작업의 일시 중지 상태를 복원했습니다. 이번 실행을 취소했으며 정상적인 경우에는 복원 단계에 실행 중인 에이전트가 없습니다")
	AbortGoalMet = cause("goal_met", "계획자가 작업 목표 달성을 판정했습니다",
		"계획자가 작업 목표가 달성되었다고 판정하여 작업을 done으로 설정하고 아직 실행 중인 Worker를 취소했습니다. 해당 의도는 실패가 아니라 stopped로 표시됩니다")
	AbortSettleDrainTimeout = cause("settle_drain_timeout", "작업 시간 초과 시 마무리 대기 시간을 모두 사용했습니다",
		"작업이 timeout에 도달한 뒤 실행 중인 Worker의 정상 마무리를 기다렸지만 90초의 drain 유예 시간으로도 충분하지 않아 강제 취소했습니다. 의도는 exhausted로 표시하며 마무리 단계에서 이미 기록한 사실과 자산은 보존합니다")

	// Per-work context.
	AbortKilledByPlanner = cause("killed_by_planner", "계획자가 이 의도를 종료했습니다",
		"계획자가 kill_work를 호출하여 이 의도를 명시적으로 종료했습니다. 보통 방향이 잘못되었거나 더 진행할 가치가 없다는 의미입니다. 의도는 stopped로 표시하며 자동으로 다시 할당하지 않습니다")
	AbortWorkPausedByUser = cause("work_paused_by_user", "사용자가 이 워커 의도를 일시 중지했습니다",
		"사용자가 실행 중인 Worker를 일시 중지했습니다. 이번 호출을 취소하고 의도를 paused로 변경합니다. 이미 등록한 의도, 사실, 취약점 및 활동 기록은 모두 보존하며 재개 후 처음부터 다시 실행합니다")
	AbortWorkCancelledByUser = cause("work_cancelled_by_user", "사용자가 이 워커 의도를 삭제했습니다",
		"사용자가 실행 중인 Worker를 삭제했습니다. 이번 호출을 취소하며 Worker가 쓰기 구역을 벗어나면 서버가 사용자가 선택한 삭제 방식에 따라 처리합니다. 논리 삭제는 삭제됨으로만 표시하고 모든 산출물을 보존하며, 물리 삭제는 해당 의도와 그것에만 의존하는 하위 노드를 연쇄 삭제합니다")
	AbortWorkFinished = cause("work_finished", "Worker가 정상적으로 종료하고 context를 해제했습니다",
		"Worker가 정상적으로 종료하여 엔진이 detachWork에서 context 자원을 해제합니다. 실행 중단이 아닙니다. 이 내용이 중단 메시지에 표시되면 취소와 마무리 이벤트 사이에 경쟁 상태가 발생한 것입니다")
	AbortPausedRaceGuard = cause("paused_race_guard", "작업이 일시 중지되어 새 실행을 시작하지 않았습니다",
		"작업이 일시 중지된 동안 엔진은 새로운 실행 context를 발급하지 않습니다. claim과 일시 중지 사이의 경쟁 상태로 Worker가 계속 시작되는 것을 막기 위한 것이며 이미 할당한 의도는 frontier로 돌아갑니다")

	// Main Agent and standalone conversation contexts.
	AbortChatStoppedByUser = cause("chat_stopped_by_user", "사용자가 이번 대화를 중지했습니다",
		"사용자가 중지를 눌러 이번 주 에이전트 또는 대화 에이전트 실행을 명시적으로 중단했습니다. 이미 생성한 활동 기록은 보존하며 다음 메시지를 계속 보낼 수 있습니다")
	AbortChatPausedWithTask = cause("chat_paused_with_task", "작업 일시 중지와 함께 주 에이전트 대화를 중단했습니다",
		"사용자가 작업을 일시 중지할 때 실행 중인 주 에이전트 대화도 함께 취소했습니다. 이미 생성한 활동 기록은 보존하지만 작업을 재개해도 이번 메시지를 자동으로 다시 실행하지 않습니다")
	AbortChatTurnFinished = cause("chat_turn_finished", "이번 대화가 정상적으로 끝나고 context를 해제했습니다",
		"이번 대화가 정상적으로 끝나 서버가 해당 턴의 context 자원을 해제합니다. 실행 중단이 아닙니다. 이 내용이 중단 메시지에 표시되면 취소와 마무리 이벤트 사이에 경쟁 상태가 발생한 것입니다")

	// Process-level and per-run hard backstop.
	AbortShutdown = cause("shutdown", "백엔드 프로세스를 종료하는 중입니다",
		"백엔드 프로세스가 SIGINT 또는 SIGTERM을 받아 재시작, 갱신 또는 종료 중입니다. 실행 중인 모든 에이전트를 취소하며 재시작 후 남은 running 의도는 open으로 초기화하여 다시 실행합니다")
	AbortRunHardTimeout = cause("run_hard_timeout", "단일 실행의 강제 시간 제한이 발동했습니다",
		"단일 실행이 소프트 경과 시간 예산과 추가 유예 시간을 초과했습니다. 모델 요청이나 도구가 오랫동안 반환하지 않아 턴 경계에서 정상적으로 마무리할 수 없다는 뜻입니다. 중단 전에 마지막으로 반환하지 않은 도구 호출을 우선 확인하세요")
)

// AbortReason resolves the named cause attached to a cancelled run context.
func AbortReason(ctx context.Context) (code, short, text string, ok bool) {
	c := context.Cause(ctx)
	if c == nil {
		return "", "", "", false
	}
	var ac *AbortCause
	if errors.As(c, &ac) {
		return ac.Code, ac.Short, ac.Text, true
	}
	switch {
	case errors.Is(c, context.DeadlineExceeded):
		return "deadline_exceeded", "상위 context가 deadline에 도달했습니다",
			"상위 context가 deadline에 도달했지만 설정한 측에서 WithTimeoutCause로 명명된 사유를 첨부하지 않았습니다: " + c.Error(), true
	case errors.Is(c, context.Canceled):
		return "canceled_no_cause", "취소한 측에서 명명된 사유를 첨부하지 않았습니다",
			"상위 context가 취소되었지만 취소한 측에서 context.WithCancelCause로 명명된 사유를 첨부하지 않았습니다. agent/cancelcause.go에 사유를 등록하고 해당 취소 지점에 연결하세요", true
	default:
		return "other", firstLine(c.Error(), 80), c.Error(), true
	}
}
