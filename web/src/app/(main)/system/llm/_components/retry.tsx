"use client";

// LLM 重试配置的共用件：五层重试各自的「次数 + 间隔」。
//
// 五层从内到外：建连(SDK) → 空响应(SDK) → 同 provider 安全窗口 → 轮询熔断 → 意图重跑。
// 前三层跟着端点走，所以每个模型配置都能覆盖全局默认；后两层是进程级的，只有全局一份。
//
// 所有输入都遵循同一套「留空 = 不配置」语义，与后端 db.RetryRule 一致：
//   次数   空/0 = 用内置默认 | -1 = 关闭这层重试 | >0 = 用这个次数
//   间隔   空/0 = 用这层原本的指数退避 | >0 = 改用这个固定毫秒间隔

import * as React from "react";

import { Loader2Icon, SaveIcon } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { api } from "@/lib/api";
import type { LLMRetryOverride, LLMRetryPolicy, LLMRetryRule } from "@/lib/types";

export const ZERO_RULE: LLMRetryRule = { attempts: 0, interval_ms: 0 };
export const ZERO_OVERRIDE: LLMRetryOverride = {
  connect: ZERO_RULE,
  empty: ZERO_RULE,
  stream: ZERO_RULE,
};
const ZERO_POLICY: LLMRetryPolicy = {
  ...ZERO_OVERRIDE,
  breaker: ZERO_RULE,
  intent: ZERO_RULE,
};

type LayerMeta = {
  title: string;
  /** 这层重试发生在哪、由谁执行 */
  where: string;
  /** 什么样的错误会走到这层——具体到状态码，别让人猜 */
  trigger: string;
  /** 长得像但【不】走这层的错误，省得填了没反应还以为是 bug */
  skips?: string;
  desc: string;
  attemptsLabel: string;
  /** 次数留空时的默认值，用于占位符 */
  defAttempts: number;
  /** 间隔留空时的默认策略，用于占位符 */
  defInterval: string;
  /** 次数填 -1 的含义 */
  offHint: string;
};

export const RETRY_LAYERS = {
  connect: {
    title: "연결 재시도",
    where: "SDK · 200 응답을 받기 전",
    trigger:
      "연결하지 못했거나 아직 200 응답을 받지 못한 경우입니다. 연결 초기화 / 읽기·쓰기 시간 초과 / DNS 실패 등의 네트워크 계층 오류 및 HTTP 408, 429, 500, 502, 503, 504가 해당합니다.",
    skips: "나머지 상태 코드(400 / 401 / 403 / 404 / 413 / 422 등)는 확정적인 거부이므로 재전송해도 실패하며 바로 상위 호출자에 전달합니다.",
    desc: "동일한 요청을 그대로 다시 전송합니다. 스트림이 시작되어 200 응답을 받은 뒤 연결이 끊어지는 경우는 이 계층에서 처리하지 않습니다.",
    attemptsLabel: "재시도 횟수",
    defAttempts: 3,
    defInterval: "0.5초 → 1초 → 2초 지수 증가(최대 8초)",
    offHint: "-1 = 재시도 없이 실패를 즉시 상위 호출자에 전달",
  },
  empty: {
    title: "빈 응답 재시도",
    where: "SDK · openai 형식만 지원",
    trigger:
      "HTTP 200이고 finish_reason은 정상적인 stop이지만 응답에 콘텐츠 블록이 하나도 없는 경우입니다. 게이트웨이의 빈 프레임, 추론 필드 누락 또는 샘플링의 일시적 이상 등이 해당합니다.",
    skips: "max_tokens 한도에서 잘려 내용이 없는 경우는 제외합니다. 이때는 출력 상한을 높여야 하며 재전송해도 다시 같은 문제가 발생합니다.",
    desc: "전체 프롬프트를 재전송하므로 긴 컨텍스트에서는 비용이 큽니다. 횟수를 너무 높게 설정하지 않는 것이 좋습니다.",
    attemptsLabel: "재시도 횟수",
    defAttempts: 2,
    defInterval: "0.5초 → 1초 → 2초 지수 증가(최대 8초)",
    offHint: "-1 = 빈 응답을 그대로 반환",
  },
  stream: {
    title: "동일 제공자의 안전 구간 재시도",
    where: "이 프로젝트 · 출력 전달 전",
    trigger:
      "스트림이 연결되어 200 응답을 받은 뒤 문제가 발생했지만 호출자에게 토큰을 하나도 전달하지 않은 경우입니다. 중간 연결 끊김, 제공자 overloaded, 스트림 내부의 429 / 5xx 오류 이벤트 등이 해당합니다.",
    skips:
      "할당량 소진(402 / insufficient_quota, 순환 선택으로 설정 전환), 컨텍스트 초과(413 / context length, 압축으로 처리), 400 / 401 / 403 / 404 / 422의 확정적 거부는 재시도하지 않습니다.",
    desc: "같은 설정에서 같은 요청을 재실행합니다. 아직 출력을 전달하지 않았으므로 모델 출력이나 도구 실행이 중복되지 않습니다.",
    attemptsLabel: "재시도 횟수",
    defAttempts: 2,
    defInterval: "0.5초 → 1초 지수 증가(최대 4초)",
    offHint: "-1 = 스트림 끊김을 바로 바깥 계층의 의도 재실행에 전달",
  },
  breaker: {
    title: "순환 선택 자동 차단",
    where: "이 프로젝트 · 프로세스 단위, 전역 상태 하나",
    trigger:
      "일시적 실패(429, 5xx, 네트워크 오류)가 연속으로 임계값에 도달하면 자동 차단합니다. 잔액 부족(402), 키 무효(401 / 403), 모델 없음(404)과 같은 확정적 실패는 임계값과 무관하게 첫 실패에서 차단합니다.",
    skips: "한 번 성공하면 횟수를 초기화하므로 간헐적으로 실패하는 설정의 오류가 계속 누적되어 차단되지는 않습니다.",
    desc: "자동 차단 후 재시도 대기 상태로 전환하며, 대기 중에는 순환 선택에서 해당 설정을 건너뜁니다. 상태를 데이터베이스에 저장하므로 재시작해도 유지됩니다.",
    attemptsLabel: "자동 차단까지 연속 실패 횟수",
    defAttempts: 3,
    defInterval: "1분 → 5분 → 30분 단계별 증가",
    offHint: "-1 = 일시적 실패로는 차단하지 않음(확정적 실패는 여전히 차단)",
  },
  intent: {
    title: "의도 재실행",
    where: "이 프로젝트 · 프로세스 단위, 전역 상태 하나",
    trigger:
      "앞선 계층에서 처리하지 못해 워커가 model_error로 종료한 경우입니다. 내부 재시도를 모두 소진했거나 출력을 전달하기 시작한 뒤 스트림이 끊어진 경우가 해당합니다. 후자의 경우 동일 요청 재전송은 안전하지 않아 의도 전체를 처음부터 다시 실행해야 합니다.",
    skips: "할당량 소진은 순환 선택의 설정 전환으로 처리하므로 여기에서 재실행하지 않습니다. 작업이 일시 중지 / 종료 / 마무리 상태로 전환되면 재시도 대기 시간을 소비하지 않고 즉시 제어를 넘깁니다.",
    desc: "의도 전체를 처음부터 다시 실행합니다. 가장 바깥 계층이므로 한 번 재실행하면 내부 각 계층의 재시도 횟수도 다시 적용됩니다.",
    attemptsLabel: "재실행 횟수",
    defAttempts: 2,
    defInterval: "고정 3초",
    offHint: "-1 = 재실행하지 않고 해당 의도를 바로 blocked로 판정",
  },
} satisfies Record<string, LayerMeta>;

type LayerKey = keyof typeof RETRY_LAYERS;

/** 毫秒的人话，只用于在输入框旁边回显，免得数零。 */
function humanMs(ms: number) {
  if (!Number.isFinite(ms) || ms <= 0) return "";
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60_000) return `${Number((ms / 1000).toFixed(2))}s`;
  return `${Number((ms / 60_000).toFixed(2))}min`;
}

/** 受控数字输入：空串 ↔ 0，中间态（"-"、"1e"）原样留在本地，不打扰父级。 */
function NumField({
  id,
  value,
  onChange,
  placeholder,
  min,
}: {
  id: string;
  value: number;
  onChange: (n: number) => void;
  placeholder: string;
  min: number;
}) {
  const [text, setText] = React.useState(value === 0 ? "" : String(value));
  // 父级换了一整套值（读取到策略、切换配置）时跟上；自己敲字时不会走到这里，
  // 因为那时 value 已经等于本地文本 parse 后的结果。
  React.useEffect(() => {
    const incoming = value === 0 ? "" : String(value);
    setText((cur) => (Number(cur || 0) === value ? cur : incoming));
  }, [value]);
  return (
    <Input
      id={id}
      type="number"
      min={min}
      className="w-28 shrink-0"
      value={text}
      placeholder={placeholder}
      onChange={(e) => {
        setText(e.target.value);
        const n = Number(e.target.value);
        onChange(e.target.value.trim() === "" || !Number.isFinite(n) ? 0 : Math.trunc(n));
      }}
    />
  );
}

/** 一层重试的两个旋钮。idPrefix 用来在同一页出现多次时保住 label 的 htmlFor。 */
export function RetryRuleFields({
  layer,
  idPrefix,
  value,
  onChange,
  compact,
}: {
  layer: LayerKey;
  idPrefix: string;
  value: LLMRetryRule;
  onChange: (r: LLMRetryRule) => void;
  /** true = 配置抽屉里的紧凑版：省掉展开说明，只留「什么错误会走到这层」这一句 */
  compact?: boolean;
}) {
  const meta = RETRY_LAYERS[layer];
  const human = humanMs(value.interval_ms);
  return (
    <div className={compact ? "grid gap-2" : "grid gap-3 rounded-lg border p-3"}>
      <div className="grid gap-0.5">
        <div className="flex flex-wrap items-baseline gap-2">
          <Label className="text-sm">{meta.title}</Label>
          <span className="text-muted-foreground text-xs">{meta.where}</span>
        </div>
        {/* 哪些错误会走到这层，具体到状态码——填了旋钮却看不到效果，多半是错误压根不落在这层。 */}
        <p className="text-muted-foreground text-xs">
          <span className="font-medium text-foreground">트리거</span>：{meta.trigger}
        </p>
        {!compact && meta.skips && (
          <p className="text-muted-foreground text-xs">
            <span className="font-medium text-foreground">이 계층을 사용하지 않음</span>：{meta.skips}
          </p>
        )}
        {!compact && <p className="text-muted-foreground text-xs">{meta.desc}</p>}
      </div>
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
        <div className="flex items-center gap-2">
          <Label htmlFor={`${idPrefix}-${layer}-n`} className="text-muted-foreground text-xs">
            {meta.attemptsLabel}
          </Label>
          <NumField
            id={`${idPrefix}-${layer}-n`}
            min={-1}
            value={value.attempts}
            placeholder={`기본값 ${meta.defAttempts}`}
            onChange={(n) => onChange({ ...value, attempts: n })}
          />
        </div>
        <div className="flex items-center gap-2">
          <Label htmlFor={`${idPrefix}-${layer}-ms`} className="text-muted-foreground text-xs">
            간격 ms
          </Label>
          <NumField
            id={`${idPrefix}-${layer}-ms`}
            min={0}
            value={value.interval_ms}
            placeholder="기본 재시도 대기"
            onChange={(n) => onChange({ ...value, interval_ms: n })}
          />
          <span className="text-muted-foreground text-xs">{human ? `고정 ${human}` : meta.defInterval}</span>
        </div>
      </div>
      {!compact && <p className="text-muted-foreground text-xs">비워 두면 기본값 사용; {meta.offHint}。</p>}
    </div>
  );
}

/** 模型配置抽屉里的三层覆盖（跟着端点走的那三层）。 */
export function ProfileRetryFields({
  value,
  onChange,
}: {
  value: LLMRetryOverride;
  onChange: (o: LLMRetryOverride) => void;
}) {
  return (
    <div className="grid gap-3 rounded-lg border p-3">
      <div className="grid gap-0.5">
        <Label className="text-sm">재시도 개별 설정</Label>
        <p className="text-muted-foreground text-xs">
          이 설정에만 적용하며 「재시도 및 대기 시간」의 전역 기본값을 대체합니다. 각 칸을 비워 두면 전역 설정을 따르고, 횟수에 -1을 입력하면 해당 계층의 재시도를 끕니다. 간격을 입력하면 지수 증가 대신 고정 간격을 사용합니다. 자동 차단과 의도 재실행은 프로세스 단위이므로 전역 설정 페이지에서만 변경할 수 있습니다.
        </p>
      </div>
      {(["connect", "empty", "stream"] as const).map((k) => (
        <div key={k} className="border-t pt-3 first:border-t-0 first:pt-0">
          <RetryRuleFields
            compact
            layer={k}
            idPrefix="pf"
            value={value[k]}
            onChange={(r) => onChange({ ...value, [k]: r })}
          />
        </div>
      ))}
    </div>
  );
}

/** 「重试与退避」tab：五层的全局默认值。 */
export function RetryPolicyPanel() {
  const [policy, setPolicy] = React.useState<LLMRetryPolicy>(ZERO_POLICY);
  const [loading, setLoading] = React.useState(true);
  const [saving, setSaving] = React.useState(false);

  const load = React.useCallback(async () => {
    setLoading(true);
    try {
      const p = await api.llmRetryPolicy();
      setPolicy({ ...ZERO_POLICY, ...p });
    } catch (e) {
      toast.error(`재시도 정책 읽기 실패: ${(e as Error).message}`);
    } finally {
      setLoading(false);
    }
  }, []);

  React.useEffect(() => {
    void load();
  }, [load]);

  async function save() {
    if (saving) return;
    setSaving(true);
    try {
      // 后端会把越界值夹回区间并回传，直接用回传值刷新，所见即所存。
      const saved = await api.saveLLMRetryPolicy(policy);
      setPolicy({ ...ZERO_POLICY, ...saved });
      toast.success("저장했으며 즉시 적용됩니다(현재 실행 중인 호출은 이전 매개변수 유지)");
    } catch (e) {
      toast.error(`저장 실패: ${(e as Error).message}`);
    } finally {
      setSaving(false);
    }
  }

  const set = (k: LayerKey) => (r: LLMRetryRule) => setPolicy((p) => ({ ...p, [k]: r }));

  if (loading) {
    return (
      <div className="flex items-center gap-2 rounded-lg border border-dashed p-10 text-muted-foreground text-sm">
        <Loader2Icon className="size-4 animate-spin" /> 재시도 정책 읽는 중…
      </div>
    );
  }

  return (
    <div className="grid gap-4">
      <div className="rounded-lg border bg-muted/30 p-3 text-muted-foreground text-xs leading-relaxed">
        모델 호출이 실패하면 안쪽부터 바깥쪽으로 다섯 계층의 재시도를 거칩니다:
        <span className="text-foreground"> 연결 → 빈 응답 → 동일 제공자의 안전 구간 → 순환 선택 자동 차단 → 의도 재실행</span>
        . 안쪽 재시도를 소진해야 바깥쪽으로 넘어가므로 횟수는
        <span className="text-foreground">서로 곱해집니다</span>
        . 모든 계층을 최대로 설정하면 일시적인 오류 한 번에 수십 회의 요청이 발생할 수 있습니다. 모두 비워 두면 현재 기본값을 사용하므로 이 페이지가 없을 때와 동작이 완전히 같습니다. 처음 세 계층은 모델 설정별로 개별 지정할 수 있습니다.
      </div>

      <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
        {(Object.keys(RETRY_LAYERS) as LayerKey[]).map((k) => (
          <RetryRuleFields key={k} layer={k} idPrefix="gl" value={policy[k]} onChange={set(k)} />
        ))}
      </div>

      <div className="flex gap-2">
        <Button onClick={save} disabled={saving}>
          {saving ? <Loader2Icon className="animate-spin" /> : <SaveIcon />}
          저장
        </Button>
        <Button variant="outline" onClick={() => setPolicy(ZERO_POLICY)} disabled={saving}>
          모두 기본값으로 복원
        </Button>
      </div>
    </div>
  );
}
