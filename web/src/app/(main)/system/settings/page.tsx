"use client";

import * as React from "react";

import { CpuIcon, FlaskConicalIcon, KeyboardIcon, RadioTowerIcon, SearchIcon, ShieldAlertIcon } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { api } from "@/lib/api";
import { CHAT_SEND_MODE_OPTIONS, type ChatSendMode, setChatSendMode, useChatSendMode } from "@/lib/chat-send-mode";
import type { Settings } from "@/lib/types";


export default function SystemSettingsPage() {
  const [trafficCapture, setTrafficCapture] = React.useState(false);
  const [agentTrafficBinding, setAgentTrafficBinding] = React.useState(false);
  const [webSearch, setWebSearch] = React.useState(false);
  const [backend, setBackend] = React.useState("ddgs");
  const [braveKeySet, setBraveKeySet] = React.useState(false);
  const [braveKeyInput, setBraveKeyInput] = React.useState("");
  const [tavilyKeySet, setTavilyKeySet] = React.useState(false);
  const [tavilyKeyInput, setTavilyKeyInput] = React.useState("");
  const [savingTavilyKey, setSavingTavilyKey] = React.useState(false);
  const [proxyInput, setProxyInput] = React.useState("");
  const [savingProxy, setSavingProxy] = React.useState(false);
  const [globalProxyInput, setGlobalProxyInput] = React.useState("");
  const [savingGlobalProxy, setSavingGlobalProxy] = React.useState(false);
  const [testing, setTesting] = React.useState(false);
  const [loaded, setLoaded] = React.useState(false);
  const [saving, setSaving] = React.useState(false);
  const [savingKey, setSavingKey] = React.useState(false);
  const [pyInterp, setPyInterp] = React.useState("");
  const [workers, setWorkers] = React.useState("3");
  const [savingWorkers, setSavingWorkers] = React.useState(false);
  // 操作约束注入范围(默认都开)。
  const [injectPlanner, setInjectPlanner] = React.useState(true);
  const [injectWorker, setInjectWorker] = React.useState(true);
  // 实验功能:noa 上下文压缩(默认关)。
  const [noaCompaction, setNoaCompaction] = React.useState(false);
  // 纯前端偏好：不走 /api/settings，直接读写 localStorage。
  const sendMode = useChatSendMode();

  const apply = React.useCallback((s: Settings) => {
    setTrafficCapture(!!s.traffic_capture);
    setAgentTrafficBinding(!!s.agent_traffic_binding);
    setWebSearch(!!s.web_search_enabled);
    setBackend(s.web_search_backend || "ddgs");
    setBraveKeySet(!!s.brave_key_set);
    setTavilyKeySet(!!s.tavily_key_set);
    setProxyInput(s.web_search_proxy ?? "");
    setGlobalProxyInput(s.global_proxy ?? "");
    setPyInterp(s.python_interpreter ?? "");
    setWorkers(String(s.workers ?? 3));
    setInjectPlanner(s.constraints_inject_planner !== false);
    setInjectWorker(s.constraints_inject_worker !== false);
    setNoaCompaction(!!s.noa_compaction);
  }, []);

  const saveWorkers = () => {
    const n = Number(workers);
    if (!Number.isInteger(n) || n <= 0) {
      toast.error("동시 실행 수는 0보다 큰 정수여야 합니다");
      return;
    }
    setSavingWorkers(true);
    api
      .setSettings({ workers: n })
      .then((s) => {
        apply(s);
        toast.success("동시 실행 워커 에이전트 수를 저장했습니다(이후 시작하는 작업에 적용)");
      })
      .catch((e) => toast.error("저장 실패: " + (e as Error).message))
      .finally(() => setSavingWorkers(false));
  };

  const savePython = () => {
    setSaving(true);
    api
      .setSettings({ python_interpreter: pyInterp.trim() })
      .then((s) => {
        apply(s);
        toast.success("Python 인터프리터 설정을 저장했습니다");
      })
      .catch((e) => toast.error("저장 실패: " + (e as Error).message))
      .finally(() => setSaving(false));
  };
  const detectPython = () => {
    setSaving(true);
    api
      .detectPython()
      .then((r) => setPyInterp(r.python_interpreter))
      .catch(() => undefined)
      .finally(() => setSaving(false));
  };

  React.useEffect(() => {
    api
      .settings()
      .then(apply)
      .catch(() => undefined)
      .finally(() => setLoaded(true));
  }, [apply]);

  const toggleTraffic = (v: boolean) => {
    setTrafficCapture(v); // optimistic
    setSaving(true);
    api
      .setSettings({ traffic_capture: v })
      .then(apply)
      .catch(() => setTrafficCapture(!v)) // revert on failure
      .finally(() => setSaving(false));
  };

  const toggleInjectPlanner = (v: boolean) => {
    setInjectPlanner(v); // optimistic
    api
      .setSettings({ constraints_inject_planner: v })
      .then(apply)
      .catch(() => setInjectPlanner(!v)); // revert on failure
  };

  const toggleAgentTrafficBinding = (v: boolean) => {
    setAgentTrafficBinding(v);
    setSaving(true);
    api
      .setSettings({ agent_traffic_binding: v })
      .then((s) => {
        apply(s);
        toast.success(v ? "에이전트의 자동 트래픽 연결을 활성화했습니다" : "에이전트의 자동 트래픽 연결을 비활성화했습니다");
      })
      .catch((e) => {
        setAgentTrafficBinding(!v);
        toast.error(`저장 실패: ${(e as Error).message}`);
      })
      .finally(() => setSaving(false));
  };

  const toggleInjectWorker = (v: boolean) => {
    setInjectWorker(v); // optimistic
    api
      .setSettings({ constraints_inject_worker: v })
      .then(apply)
      .catch(() => setInjectWorker(!v)); // revert on failure
  };

  const toggleNoaCompaction = (v: boolean) => {
    setNoaCompaction(v); // optimistic
    api
      .setSettings({ noa_compaction: v })
      .then((s) => {
        apply(s);
        toast.success(v ? "noa 컨텍스트 압축을 활성화했습니다(이후 시작하는 실행에 적용)" : "noa 컨텍스트 압축을 비활성화했습니다(내장 압축으로 복귀)");
      })
      .catch((e) => {
        setNoaCompaction(!v); // revert on failure
        toast.error(`저장 실패: ${(e as Error).message}`);
      });
  };

  // Persist a web-search patch (enable and/or backend). Optimistic with refetch.
  const saveWebSearch = (patch: Partial<Settings>) => {
    setSaving(true);
    api
      .setSettings(patch)
      .then((s) => {
        apply(s);
        toast.success("웹 검색 설정을 저장했습니다");
      })
      .catch((e) => {
        toast.error("저장 실패: " + (e as Error).message);
        api
          .settings()
          .then(apply)
          .catch(() => undefined);
      })
      .finally(() => setSaving(false));
  };

  const saveBraveKey = () => {
    setSavingKey(true);
    api
      .setSettings({ brave_search_api_key: braveKeyInput })
      .then((s) => {
        apply(s);
        setBraveKeyInput("");
        toast.success("Brave API Key를 저장했습니다");
      })
      .catch((e) => toast.error("저장 실패: " + (e as Error).message))
      .finally(() => setSavingKey(false));
  };

  const saveTavilyKey = () => {
    setSavingTavilyKey(true);
    api
      .setSettings({ tavily_search_api_key: tavilyKeyInput })
      .then((s) => {
        apply(s);
        setTavilyKeyInput("");
        toast.success("Tavily API Key를 저장했습니다");
      })
      .catch((e) => toast.error("저장 실패: " + (e as Error).message))
      .finally(() => setSavingTavilyKey(false));
  };

  const saveProxy = () => {
    setSavingProxy(true);
    api
      .setSettings({ web_search_proxy: proxyInput.trim() })
      .then((s) => {
        apply(s);
        toast.success(proxyInput.trim() ? "외부 연결 프록시를 저장했습니다" : "외부 연결 프록시를 지웠습니다(직접 연결로 변경)");
      })
      .catch((e) => toast.error("저장 실패: " + (e as Error).message))
      .finally(() => setSavingProxy(false));
  };

  const saveGlobalProxy = () => {
    setSavingGlobalProxy(true);
    api
      .setSettings({ global_proxy: globalProxyInput.trim() })
      .then((s) => {
        apply(s);
        toast.success(globalProxyInput.trim() ? "전역 프록시를 저장했습니다" : "전역 프록시를 지웠습니다(직접 연결로 변경)");
      })
      .catch((e) => toast.error("저장 실패: " + (e as Error).message))
      .finally(() => setSavingGlobalProxy(false));
  };

  // Run a real "test" search ("test") against the CURRENT form values (backend +
  // proxy + entered key), falling back to saved values server-side. Toasts result.
  const runTest = () => {
    setTesting(true);
    api
      .testWebSearch({
        web_search_backend: backend,
        web_search_proxy: proxyInput.trim(),
        brave_search_api_key: braveKeyInput,
        tavily_search_api_key: tavilyKeyInput,
      })
      .then((r) => {
        if (r.ok) toast.success(`검색 테스트 성공 · ${r.backend} 반환한 항목:  ${r.count} 개의 결과`);
        else toast.error("검색 테스트 실패: " + (r.error || "알 수 없는 오류"));
      })
      .catch((e) => toast.error("검색 테스트 실패: " + (e as Error).message))
      .finally(() => setTesting(false));
  };

  // brave-free selected but no key stored and none being entered → tool stays off.
  const braveNeedsKey = webSearch && backend === "brave-free" && !braveKeySet;

  return (
    <div className="flex flex-1 flex-col gap-4 md:gap-6">
      <div>
        <h1 className="text-xl font-semibold tracking-tight">시스템 설정</h1>
        <p className="text-muted-foreground text-sm">전역 실행 설정 스위치</p>
      </div>

      {/* 多列而非 grid：网络搜索卡片比其余高数倍，且高度随所选后端变化（brave/tavily
          的 key 输入是条件渲染）。grid 会按最高的一张撑满整行、在旁边留下大片空白，
          多列则自动按内容高度平衡填充。卡片间距靠 mb 而非 gap——多列布局下
          column-gap 只管列间距，行间距要由子元素自己给。 */}
      <div className="columns-1 gap-4 md:gap-6 lg:columns-2">

        <Card className="mb-4 break-inside-avoid md:mb-6">
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <RadioTowerIcon className="size-4" />
              트래픽 캡처
            </CardTitle>
            <CardDescription>
              활성화하면 모든 에이전트의 HTTP 트래픽을 기록 프록시를 통해 전부 저장하고, 에이전트에 traffic_search / traffic_get 도구와 프록시 설정을 주입합니다(프롬프트에 프록시 설명 포함).
              <br />
              비활성화(기본값)하면 트래픽을 기록하지 않습니다. 에이전트에
              <b>제공하지 않습니다: </b>프록시 설정과 트래픽 도구. 프롬프트에도 <b>포함하지 않습니다: </b>프록시 관련 내용. 설정 변경 시 에이전트를 즉시 다시 구성하여 적용합니다.
            </CardDescription>
          </CardHeader>
          <CardContent className="flex items-center justify-between gap-4">
            <Label htmlFor="traffic-capture" className="text-sm font-normal text-muted-foreground">
              {trafficCapture ? "활성화됨 · 트래픽 기록 및 프록시 주입 중" : "비활성화됨 · 트래픽 기록 및 프록시 주입 없음"}
            </Label>
            <Switch
              id="traffic-capture"
              checked={trafficCapture}
              disabled={!loaded || saving}
              onCheckedChange={toggleTraffic}
            />
          </CardContent>
        </Card>

        <Card className="mb-4 break-inside-avoid md:mb-6">
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <RadioTowerIcon className="size-4" />
              에이전트 자동 트래픽 연결
            </CardTitle>
            <CardDescription id="agent-traffic-binding-description">
              기본값은 비활성화입니다. 활성화하면 취약점 저장 시 실행되는 보고서 에이전트가 기존 HTTP 요청/응답을 확인하고 해당 트래픽을 연결한 뒤 보고서를 작성합니다.
              <b>패킷 조회와 추가 도구 호출은 토큰 사용량을 늘립니다.</b>
              <br />
              TCP이거나 캡처하지 않았거나 일치하는 트래픽이 없어도 정상적으로 보고할 수 있습니다. 이 스위치는 트래픽 캡처, 수동 연결 및 저장된 증거 조회에 영향을 주지 않습니다. 다음 에이전트 실행부터 적용하며, 비활성화하면 새로운 자동 연결을 즉시 거부합니다.
            </CardDescription>
          </CardHeader>
          <CardContent className="flex items-center justify-between gap-4">
            <Label htmlFor="agent-traffic-binding" className="text-sm font-normal text-muted-foreground">
              {agentTrafficBinding ? "활성화됨 · 토큰 사용량 증가" : "비활성화됨 · 수동 연결은 계속 가능"}
            </Label>
            <Switch
              id="agent-traffic-binding"
              aria-describedby="agent-traffic-binding-description"
              checked={agentTrafficBinding}
              disabled={!loaded || saving}
              onCheckedChange={toggleAgentTrafficBinding}
            />
          </CardContent>
        </Card>

        <Card className="mb-4 break-inside-avoid md:mb-6">
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <RadioTowerIcon className="size-4" />
              전역 프록시
            </CardTitle>
            <CardDescription>
              모든 에이전트의 <b>대상 트래픽</b>은 이 프록시를 통해 외부로 연결됩니다(원본 IP 숨기기 / 경유 호스트 사용). 지원 형식:  <b>http / https / socks5</b>, 지원하는 인증: {" "}
              <code>user:pass</code> . 비워 두면 직접 연결합니다.
              <br />
              활성화<b>트래픽 캡처</b>상태에서는 기록 프록시의 <b>상위 프록시</b>로 동작합니다(트래픽을 전부 저장한 뒤 이 프록시로 외부 연결). 캡처를 끄면 에이전트의 bash / WebFetch 외부 연결에 직접 주입합니다. 웹 검색 프록시 및 LLM 프록시와는 독립적입니다.
              <br />
              <b>힌트</b>: socks5는 <b>캡처가 꺼진</b>상태에서 각 명령행 도구의  <code>ALL_PROXY</code> 지원에 의존합니다(curl은 지원하지만 일부 도구는 무시할 수 있음). 주로 socks5를 사용한다면 트래픽 캡처를 활성화하는 것이 좋습니다. 이 경로에서는 MITM이 직접 연결하므로 도구의 인식 여부와 무관하게 안정적으로 적용됩니다.
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-2">
            <Label htmlFor="global-proxy" className="text-sm font-normal text-muted-foreground">
              프록시 주소
            </Label>
            <div className="flex items-center gap-2">
              <Input
                id="global-proxy"
                autoComplete="off"
                placeholder="socks5://user:pass@host:1080 또는 http://host:port(비워 두면 직접 연결)"
                value={globalProxyInput}
                disabled={!loaded || savingGlobalProxy}
                onChange={(e) => setGlobalProxyInput(e.target.value)}
              />
              <Button type="button" onClick={saveGlobalProxy} disabled={!loaded || savingGlobalProxy}>
                저장
              </Button>
            </div>
            <p className="text-muted-foreground text-xs">
              {globalProxyInput.trim() ? "설정됨 · 모든 대상 트래픽이 이 프록시를 통해 외부 연결" : "설정되지 않음 · 대상 트래픽이 외부로 직접 연결"}
            </p>
          </CardContent>
        </Card>

        <Card className="mb-4 break-inside-avoid md:mb-6">
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <ShieldAlertIcon className="size-4" />
              동작 제약 주입
            </CardTitle>
            <CardDescription>
              활성화하면 각 작업의 <b>동작 제약</b>(작업 개요의 「동작 제약」에서 관리하는 allow/deny 항목)을 해당 에이전트의 시스템 프롬프트에 추가하여 탐색 경계를 정합니다(예: 「현재 포트만 테스트」, 「무차별 대입 금지」).
              <br />
              주입 대상을 각각 설정할 수 있습니다:  <b>계획자(planner)</b>및 <b>실행자(worker)</b>
              . 기본적으로 둘 다 활성화됩니다. 변경 사항은 다음 턴에 읽을 때 즉시 적용되므로 에이전트를 다시 구성할 필요가 없습니다. 비활성화하면 해당 에이전트는 제약을 더 이상 보지 못합니다.
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            <div className="flex items-center justify-between gap-4">
              <Label htmlFor="inject-planner" className="text-sm font-normal text-muted-foreground">
                계획자(planner)에 주입{injectPlanner ? " · 활성화됨" : " · 비활성화됨"}
              </Label>
              <Switch
                id="inject-planner"
                checked={injectPlanner}
                disabled={!loaded}
                onCheckedChange={toggleInjectPlanner}
              />
            </div>
            <div className="flex items-center justify-between gap-4">
              <Label htmlFor="inject-worker" className="text-sm font-normal text-muted-foreground">
                실행자(worker)에 주입{injectWorker ? " · 활성화됨" : " · 비활성화됨"}
              </Label>
              <Switch
                id="inject-worker"
                checked={injectWorker}
                disabled={!loaded}
                onCheckedChange={toggleInjectWorker}
              />
            </div>
          </CardContent>
        </Card>

        <Card className="mb-4 break-inside-avoid md:mb-6">
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <FlaskConicalIcon className="size-4" />
              실험 기능
            </CardTitle>
            <CardDescription>
              아직 검증 중인 기능으로 기본적으로 비활성화됩니다. 에이전트의 동작이나 안정성에 영향을 줄 수 있으므로 영향을 이해한 뒤 활성화하세요.
              <br />
              <b>noa 컨텍스트 압축</b>: 모델이 긴 대화 기록을 직접 압축합니다(norma v0.4.0). 활성화하면 플랫폼의 네 종류 에이전트(
              <b>계획자 / 실행자 / 주 에이전트 / 대화</b>)가 내장 압축 대신 noa로 컨텍스트를 관리합니다. 압축한 원문은 추적할 수 있도록 작업 디렉터리에 보관합니다. 이후 시작하는 실행에 즉시 적용되며 에이전트를 다시 구성할 필요가 없습니다. 비활성화하면 즉시 내장 압축으로 복귀합니다.
            </CardDescription>
          </CardHeader>
          <CardContent className="flex items-center justify-between gap-4">
            <Label htmlFor="noa-compaction" className="text-sm font-normal text-muted-foreground">
              noa 컨텍스트 압축{noaCompaction ? " · 활성화됨" : " · 비활성화됨"}
            </Label>
            <Switch
              id="noa-compaction"
              checked={noaCompaction}
              disabled={!loaded}
              onCheckedChange={toggleNoaCompaction}
            />
          </CardContent>
        </Card>

        <Card className="mb-4 break-inside-avoid md:mb-6">
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <SearchIcon className="size-4" />
              웹 검색
            </CardTitle>
            <CardDescription>
              웹 검색의 <b>전체 스위치 + 소스 설정</b>입니다. 활성화해야 <b>각 에이전트 설정</b>에서 개별적으로 활성화 여부를 선택할 수 있습니다:
              <b>web_search</b>(제목/링크/요약만 반환하며 본문은 가져오지 않음. 본문 가져오기는 WebFetch 담당). 웹 검색은 <b>사용하지 않습니다: </b>
              기록 프록시. 트래픽 캡처와 독립적입니다.
              <br />
              선택 가능한 소스:  <b>DuckDuckGo（ddgs）</b>(키 불필요), <b>Brave(무료 버전)</b>(Brave API Key 필요), {" "}
              <b>Tavily</b>(Tavily API Key 필요) 또는  <b>DeepSeek</b>(현재 LLM 설정 재사용). 전체 스위치를 끄면 각 에이전트의 웹 검색 스위치를 사용할 수 없습니다.
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            <div className="flex items-center justify-between gap-4">
              <Label htmlFor="web-search" className="text-sm font-normal text-muted-foreground">
                {webSearch ? "전체 활성화됨 · 각 에이전트 설정에서 개별 활성화 가능" : "비활성화됨 · 각 에이전트에서 웹 검색 활성화 불가"}
              </Label>
              <Switch
                id="web-search"
                checked={webSearch}
                disabled={!loaded || saving}
                onCheckedChange={(v) => {
                  setWebSearch(v); // optimistic
                  saveWebSearch({ web_search_enabled: v });
                }}
              />
            </div>

            {webSearch && (
              <div className="flex items-center justify-between gap-4">
                <Label className="text-sm font-normal text-muted-foreground">검색 소스</Label>
                <Select
                  value={backend}
                  disabled={!loaded || saving}
                  onValueChange={(v) => {
                    setBackend(v); // optimistic
                    saveWebSearch({ web_search_backend: v });
                  }}
                >
                  <SelectTrigger className="w-48 shrink-0">
                    <SelectValue placeholder="소스 선택" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="ddgs">DuckDuckGo(ddgs · 무료, 키 불필요)</SelectItem>
                    <SelectItem value="brave-free">Brave(무료 버전 · 키 필요)</SelectItem>
                    <SelectItem value="tavily">Tavily(키 필요)</SelectItem>
                    <SelectItem value="deepseek">DeepSeek(공식)</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            )}

            {webSearch && backend === "deepseek" && (
              <div className="border-border/60 bg-muted/30 flex flex-col gap-2 rounded-md border p-3">
                <p className="text-sm font-medium">DeepSeek 공식 웹 검색</p>
                <p className="text-muted-foreground text-xs leading-relaxed">
                  이 소스는 다음 설정을 직접 재사용합니다: <b>현재 활성 LLM 설정</b>. 따라서
                  <b>DeepSeek 공식 모델만 지원</b>하며 해당 설정은 <b>anthropic 프로토콜을 사용해야 합니다</b>
                  — DeepSeek의 OpenAI 프로토콜 엔드포인트는 서버 측 검색을 지원하지 않습니다. LLM 설정을 바꾸면 이 소스를 사용할 수 없게 될 수 있습니다.
                </p>
                <p className="text-muted-foreground text-xs leading-relaxed">
                  다른 소스와 달리 검색은  <b>DeepSeek 서버에서 실행</b>합니다. 검색마다 모델 호출이 한 번 추가되어 토큰 비용이 발생합니다. 검색 요청은 <b>위의 외부 연결 프록시를 거치지 않으며</b>또한 <b>트래픽 기록에도 포함하지 않습니다</b>. 반환 결과에는 <b>제목과 링크만 포함</b>
                  합니다(요약 없음). 본문이 필요하면 WebFetch로 가져옵니다.
                </p>
                <p className="text-muted-foreground text-xs leading-relaxed">
                  위 조건의 충족 여부는 직접 확인해야 하며 시스템이 차단하지는 않습니다. 아래 「검색 테스트」 버튼으로 실제 검색을 한 번 실행하여 확인할 수 있습니다.
                </p>
              </div>
            )}

            {webSearch && backend === "brave-free" && (
              <div className="flex flex-col gap-2">
                <Label htmlFor="brave-key" className="text-sm font-normal text-muted-foreground">
                  Brave Search API 키
                  {braveKeySet && <span className="ml-2 text-xs text-emerald-500">설정됨</span>}
                </Label>
                <div className="flex items-center gap-2">
                  <Input
                    id="brave-key"
                    type="password"
                    autoComplete="off"
                    placeholder={braveKeySet ? "설정됨(비워 두면 변경하지 않음)" : "Brave API Key 입력"}
                    value={braveKeyInput}
                    disabled={!loaded || savingKey}
                    onChange={(e) => setBraveKeyInput(e.target.value)}
                  />
                  <Button
                    type="button"
                    onClick={saveBraveKey}
                    disabled={!loaded || savingKey || braveKeyInput.trim() === ""}
                  >
                    저장
                  </Button>
                </div>
                {braveNeedsKey && (
                  <p className="text-xs text-amber-500">
                    Brave를 선택했지만 키를 설정하지 않았습니다. 키를 저장하기 전에는 검색 도구가 활성화되지 않습니다.
                  </p>
                )}
                <p className="text-muted-foreground text-xs">
                  무료 버전의 할당량은 월 약 2,000회입니다. https://brave.com/search/api/ 에서 키를 발급받으세요.
                </p>
              </div>
            )}

            {webSearch && backend === "tavily" && (
              <div className="flex flex-col gap-2">
                <Label htmlFor="tavily-key" className="text-sm font-normal text-muted-foreground">
                  Tavily Search API 키
                  {tavilyKeySet && <span className="ml-2 text-xs text-emerald-500">설정됨</span>}
                </Label>
                <div className="flex items-center gap-2">
                  <Input
                    id="tavily-key"
                    type="password"
                    autoComplete="off"
                    placeholder={tavilyKeySet ? "설정됨(비워 두면 변경하지 않음)" : "Tavily API Key 입력(tvly-…)"}
                    value={tavilyKeyInput}
                    disabled={!loaded || savingTavilyKey}
                    onChange={(e) => setTavilyKeyInput(e.target.value)}
                  />
                  <Button
                    type="button"
                    onClick={saveTavilyKey}
                    disabled={!loaded || savingTavilyKey || tavilyKeyInput.trim() === ""}
                  >
                    저장
                  </Button>
                </div>
                {webSearch && backend === "tavily" && !tavilyKeySet && (
                  <p className="text-xs text-amber-500">
                    Tavily를 선택했지만 키를 설정하지 않았습니다. 키를 저장하기 전에는 검색 도구가 활성화되지 않습니다.
                  </p>
                )}
                <p className="text-muted-foreground text-xs">https://tavily.com 에 가입하여 API Key를 발급받으세요.</p>
              </div>
            )}

            {webSearch && (
              <div className="flex flex-col gap-2">
                <Label htmlFor="ws-proxy" className="text-sm font-normal text-muted-foreground">
                  외부 연결 프록시(선택)
                </Label>
                <div className="flex items-center gap-2">
                  <Input
                    id="ws-proxy"
                    autoComplete="off"
                    placeholder="http://host:port 또는 socks5://host:port(비워 두면 직접 연결)"
                    value={proxyInput}
                    disabled={!loaded || savingProxy}
                    onChange={(e) => setProxyInput(e.target.value)}
                  />
                  <Button type="button" onClick={saveProxy} disabled={!loaded || savingProxy}>
                    저장
                  </Button>
                </div>
                <p className="text-muted-foreground text-xs">
                  검색 엔드포인트 접근에만 사용하는 별도 외부 연결 프록시입니다(VPN/SOCKS 등). 트래픽 기록용 MITM 프록시와 무관하며 직접 연결이 되지 않을 때 이 프록시를 사용합니다.
                </p>
              </div>
            )}

            {webSearch && (
              <div className="flex items-center justify-between gap-4 border-t pt-4">
                <p className="text-muted-foreground text-xs">
                  현재 설정(소스 + 프록시 + 키)으로 「test」를 실제로 한 번 검색하여 사용 가능 여부를 검증합니다.
                </p>
                <Button
                  type="button"
                  variant="outline"
                  onClick={runTest}
                  disabled={!loaded || testing}
                  className="shrink-0"
                >
                  {testing ? "테스트 중…" : "검색 테스트"}
                </Button>
              </div>
            )}
          </CardContent>
        </Card>

        <Card className="mb-4 break-inside-avoid md:mb-6">
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <RadioTowerIcon className="size-4" />
              사용자 지정 스크립트 · Python 인터프리터
            </CardTitle>
            <CardDescription>
              사용자 지정 <b>script</b> 유형의 도구에서 Python을 실행할 때 사용합니다. 시작 시 자동으로 감지하며 python3를 우선합니다. 가상 환경 또는 특정 버전의 절대 경로를 직접 입력할 수 있고, 비워 두면 실행 시 자동으로 감지합니다.
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            <div className="flex items-center gap-2">
              <Input
                className="font-mono text-sm"
                placeholder="/usr/bin/python3(비워 두면 자동 감지)"
                value={pyInterp}
                disabled={!loaded || saving}
                onChange={(e) => setPyInterp(e.target.value)}
              />
              <Button variant="outline" onClick={detectPython} disabled={!loaded || saving}>
                다시 감지
              </Button>
              <Button onClick={savePython} disabled={!loaded || saving}>
                저장
              </Button>
            </div>
          </CardContent>
        </Card>

        <Card className="mb-4 break-inside-avoid md:mb-6">
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <CpuIcon className="size-4" />
              동시 작업 · 워커 에이전트 수
            </CardTitle>
            <CardDescription>
              각 작업에서 동시에 실행할 워커 에이전트 수입니다(기본값 3). 값이 클수록 동시 탐지와 사용량이 늘어납니다. 변경 사항은
              <b>이후 시작하는 작업에 적용</b>되며 실행 중인 작업에는 영향을 주지 않습니다.
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            <div className="flex items-center gap-2">
              <Input
                type="number"
                min={1}
                className="w-32 font-mono text-sm"
                placeholder="3"
                value={workers}
                disabled={!loaded || savingWorkers}
                onChange={(e) => setWorkers(e.target.value)}
              />
              <Button onClick={saveWorkers} disabled={!loaded || savingWorkers}>
                저장
              </Button>
            </div>
          </CardContent>
        </Card>

        <Card className="mb-4 break-inside-avoid md:mb-6">
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <KeyboardIcon className="size-4" />
              대화 입력창 전송 단축키
            </CardTitle>
            <CardDescription>
              대화 페이지와 작업 상세 페이지의 주 에이전트 입력창이 이 설정을 공유합니다. 선택 즉시 적용되므로 저장할 필요가 없습니다.
              <br />
              이 환경 설정은 <b>현재 브라우저에만 저장</b>되며 계정과 동기화되지 않습니다. 브라우저를 바꾸거나 사이트 데이터를 삭제하면 다시 설정해야 합니다.
            </CardDescription>
          </CardHeader>
          <CardContent className="flex items-center justify-between gap-4">
            <Label htmlFor="chat-send-mode" className="text-sm font-normal text-muted-foreground">
              전송 방식
            </Label>
            <Select value={sendMode} onValueChange={(v) => setChatSendMode(v as ChatSendMode)}>
              <SelectTrigger id="chat-send-mode" className="w-72">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {CHAT_SEND_MODE_OPTIONS.map((option) => (
                  <SelectItem key={option.value} value={option.value}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
