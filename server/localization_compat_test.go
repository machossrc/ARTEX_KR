package server

import "testing"

func TestLocalizedFallbackDirectives(t *testing.T) {
	for _, tc := range []struct{ input, kind, text string }{
		{"의도 대상 확인", "intent", "대상 확인"},
		{"意图 检查目标", "intent", "检查目标"},
		{"INTENT check target", "intent", "check target"},
		{"힌트 기존 증거 사용", "hint", "기존 증거 사용"},
		{"提示 使用已有证据", "hint", "使用已有证据"},
		{"hint use existing evidence", "hint", "use existing evidence"},
		{"진행 상황", "", ""},
	} {
		kind, text := parseFallbackDirective(tc.input)
		if kind != tc.kind || text != tc.text {
			t.Errorf("%q: (%q,%q), want (%q,%q)", tc.input, kind, text, tc.kind, tc.text)
		}
	}
}

func TestLocalizedLogLevelsPreserveLegacy(t *testing.T) {
	for _, tc := range []struct{ text, level string }{
		{"연결 실패", "error"}, {"활동 기록 폐기", "error"}, {"요청 거부", "error"},
		{"连接失败", "error"}, {"丢弃记录", "error"}, {"拒绝执行", "error"},
		{"재시도", "warn"}, {"비활성화", "warn"}, {"건너뜀", "warn"},
		{"重试", "warn"}, {"disabled", "warn"}, {"정상 완료", "info"},
	} {
		if got := levelOf(tc.text); got != tc.level {
			t.Errorf("%q: %q, want %q", tc.text, got, tc.level)
		}
	}
}

func TestPythonProbeRejectsMissingExecutable(t *testing.T) {
	if usablePython("artex-nonexistent-python-executable") {
		t.Fatal("missing executable accepted")
	}
}
