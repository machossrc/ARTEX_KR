# api-recon — 참고 설명서

Grep 방법, `config.json` 템플릿 및 문제 해결입니다. 모든 grep은 `js/` 디렉터리에서 실행합니다. bundle이 한 줄이면 먼저 `js-beautify` 또는 `sed 's/}/}\n/g'`를 사용할 수 있지만, 보통 주변 범위를 포함한 raw grep이면 충분합니다.

## 스크립트 설명

`scripts/`의 모든 파일은 **참고 템플릿**이며 실행 전에 대상 사이트에 맞게 조정해야 합니다. 대표적인 수정 지점:

| 스크립트 | 주로 조정할 항목 |
|---|---|
| `harvest_static.py` | endpoint 정규식, webpack/Vite manifest 파싱, 마이크로 프런트엔드 publicPath, 재시도/동시 실행 |
| `runtime_harvest.js` | neutralize 필드명과 성공값, stub 일치 규칙과 body 구조, routes 출처, WS 기록, `waitUntil`/`routeTimeout`/`proxy` |
| `preload.js` | `loginPathRe`, L1 stubs, `neutralize.fields`, `apiPattern`, L3 활성화 여부, `recordDetail`, `observe.*`, `neutralizeVueRouter` |
| `spider_mpa.py` | `--exclude` 파괴적 링크 제외, cookie, depth/max, 동일 도메인 필터 |
| `extract_route_map.py` | `routeMap` / `routeLink` 정규식, KEY 이름 패턴 |
| `build_perm_tree.py` | `userRouteAuth` 파싱, `ROOTS`/`PREFIX_PARENT` 계층 추론, stub 외부 필드명 |
| `config.json` | 위의 모든 사이트 전용 매개변수를 관리하는 공통 설정 |

조정한 파일은 작업 디렉터리(예: `recon/`)에 저장하고 보고서에 참고 스크립트 대비 구체적인 변경점을 적는 것을 권장합니다.

---

## A. 세 관문 역분석

### A1. 렌더링 관문 — 「로그인 여부를 어떻게 판단하는가?」

```bash
grep -rhoaE '.{0,40}(isLogin|isAuthenticated|loggedIn|hasLogin|requireAuth)\b.{0,80}' js | head
grep -rhoaE 'function (getUser|getToken|getAuth)[0-9]?\([^)]*\)\{.{0,200}' js | head
grep -rhoaE '(localStorage|sessionStorage)\.getItem\("[^"]+"\)' js | sort -u
grep -rhoaE '(Cookies?|cookie)\.(get|load)\("[^"]+"\)' js | sort -u
grep -rhoaE '\batob\(|JSON\.parse\(|jwt|decode' js | head
```

`isLogin = f(getUser())` → `getUser = decode(storage.read(KEY))` 경로를 찾아 **저장 키**, **컨테이너**(Cookie vs localStorage), **인코딩**을 확인합니다.

| 인코딩 | config 위조 방식 |
|---|---|
| 평문 문자열 / `"1"` / token | `"value": "anything-truthy"` |
| `JSON.parse(x)` | `"value": "json:{\"id\":1,\"username\":\"admin\"}"` |
| `JSON.parse(atob(x))` | `"value": "b64json:{\"id\":1,\"username\":\"admin\"}"` |
| JWT | 서명 없음/`alg:none` JWT 또는 bundle의 키로 서명 |
| 암호화(SM2/AES/RSA) | 하드코딩 키 찾기. 렌더링 관문이 디코딩 가능한 blob만 요구하면 forge 가능, 아니면 정적 분석으로 대체 |

→ `cookies` / `localStorage`에 기록합니다.

### A2. 인터셉터 관문 — 「무엇이 /login 이동을 유발하는가?」

```bash
grep -rhoaE '.{0,60}(interceptors\.response|axios|request\.use).{0,120}' js | head
grep -rhoaE '.{0,40}(response_code|errcode|errno|\bcode\b|\bret\b|\bstatus\b)\s*[=!]==?\s*[\-0-9]{1,4}.{0,60}' js | head -20
grep -rhoaE '.{0,40}(未登录|请重新登录|登录已过期|unauthorized|登录失效|授权|token.{0,10}invalid).{0,40}' js | head
grep -rhoaE '.{0,30}(location\.href|router\.(push|replace)|navigate)\([^)]*login[^)]*\)' js | head
```

**필드명**, **성공값**(대개 `0` 또는 `200`), **이동을 유발하는 실패값**을 확인합니다. junk session으로 검증합니다.

```bash
curl -sk -X POST -H 'Cookie: <fakekey>=junk' https://target/api/<protected> -d '{}' -H 'Content-Type: application/json'
```

→ `neutralize.fields` + `neutralize.success`에 기록합니다.

### A3. 콘텐츠 관문 — 「메뉴/권한은 어디서 오는가?」

```bash
grep -rhoaE '"/api[^"]*(permission|perm|role|menu|acl|resource|nav)[^"]*"' js | sort -u
grep -rhoaE '.{0,30}(menus|permissions|menuList|routeList|authList|role_permissions)\b.{0,120}' js | head
grep -rhoaE 'userRouteAuth|getResultTree|routeMap|routeLink|hasPermission|checkAuth' js | head
grep -rhoaE '([A-Z_][A-Z0-9_]*):\{name:"[^"]*",link:"/[^"]+"\}' js | head
```

**2계층 데이터**(일반적인 기업 관리 화면):

| API | 일반적인 payload | 사용처 |
|---|---|---|
| `.../role_permissions` | `{ permissions: string[], role_type }` | 라우트 가드, 버튼 수준 ACL |
| `.../permissions/all` | `tree[{ code, position, children }]` | 사이드바 메뉴 렌더링 |
| bundle의 `userRouteAuth` | `{ CODE: { url, name? } }` | code → 프런트엔드 path |
| bundle의 `routeMap` | `{ KEY: { name, link } }` | 별칭 해석(webpack `o.DASHBOARD`) |

사용처 코드를 읽고 `getResultTree(tree, permissions)`의 필터 방식과 `v-if` / `hasAuth(code)`가 검사하는 필드를 확인합니다.

**수동 forge**(작은 사이트): 허용형 payload 구성 → `stubs`.

**전체 권한 트리 복원**(큰 사이트에서 사이드바/하위 모듈이 계속 비어 있을 때): **I절** 참조.

---

## B. config.json 템플릿

```json
{
  "baseUrl": "https://target/",
  "runtimeMode": "both",
  "chromium": "/usr/bin/chromium",

  "cookies": [
    { "name": "auth", "value": "b64json:{\"id\":1,\"username\":\"admin\",\"role\":\"admin\",\"func\":{},\"permissions\":[\"*\"]}" }
  ],
  "localStorage": { "token": "faketoken", "isLogin": "1" },

  "neutralize": {
    "fields": ["response_code", "code", "errno", "ret", "status"],
    "success": 0,
    "flags": { "success": true, "message": "ok" }
  },
  "forward": true,
  "loginUrlPattern": "/login",
  "apiPattern": "/api/|/rest/|/graphql",

  "mockTier": "L1+L2",
  "recordDetail": true,
  "observe": {
    "storageReads": false,
    "cookieReads": false,
    "xhrHeaders": true
  },
  "neutralizeVueRouter": true,
  "stubs": [
    {
      "match": "permissions/all|/menu|role_permissions",
      "body": {
        "response_code": 0, "code": 0,
        "data": {
          "permissions": ["*"],
          "menus": [
            { "name": "dashboard", "path": "/dashboard", "show": true, "children": [] },
            { "name": "alert", "path": "/alert", "show": true, "children": [] }
          ]
        }
      }
    }
  ],

  "explore": {
    "clickTabs": true,
    "clickTables": true,
    "pushStateFallback": true,
    "maxMenuItems": 50
  },

  "routes": ["/dashboard", "/alert", "/asset", "/device", "/report", "/config", "/system"],
  "waitMs": 1500, "perRouteMs": 900, "headless": true,
  "waitUntil": "domcontentloaded",
  "routeTimeout": 12000,
  "proxy": "",

  "captureResponses": true, "recordWs": true, "respMax": 600
}
```

필드 설명:
- `runtimeMode`：`depth`（Puppeteer）、`coverage`（browser MCP）、`both`
- `cookies[].value` 접두사: `b64json:` → base64(JSON), `json:` → 원본 JSON, 접두사 없음 → 리터럴
- `forward: true`는 실제 요청을 전달하고 코드 필드를 변경, `false`는 완전 오프라인 stub
- `mockTier`: coverage 모드에서 preload가 활성화할 계층. 예: `L1+L2`, `L1+L2+L3`
- `routes`는 `routes.txt`에서 가져오며 메뉴 forge 후 harness가 `<a href>`를 자동 추가
- `captureResponses` / `recordWs`는 depth 모드에만 적용
- `waitUntil`: 큰 SPA에서는 `domcontentloaded`를 사용하여 `networkidle2` 대기 정체 방지
- `routeTimeout`: 라우트별 `page.goto` 제한 시간(밀리초)
- `proxy`: Puppeteer `--proxy-server`. `HTTP_PROXY` / `HTTPS_PROXY`도 설정 가능

### B1. 이중 stub 템플릿(role_permissions + permissions/all)

```json
"stubs": [
  {
    "match": "role_permissions",
    "body": {
      "response_code": 0,
      "data": {
        "permissions": ["MONITOR", "MONITOR_ALERT", "THREAT", "ASSETS_RISK"],
        "role_type": "SUPER_ADMIN"
      }
    }
  },
  {
    "match": "permissions/all",
    "body": {
      "response_code": 0,
      "data": [
        {
          "code": "MONITOR",
          "position": 1,
          "children": [
            { "code": "MONITOR_ALERT", "position": 1, "children": [] }
          ]
        }
      ]
    }
  }
]
```

외부 필드명(`response_code` / `code` / `data`)은 A2 인터셉터 관문과 일치해야 하며 `permissions`는 tree의 모든 leaf code를 포함해야 합니다.

---

## C. coverage 모드: preload 설정

`scripts/preload.js` 상단의 `CONFIG` 객체를 편집하거나 CDP 삽입 전에 교체합니다.

```javascript
const CONFIG = {
  loginPathRe: /\/(login|signin)(\/|$|\?)/i,
  mockTier: 'L1+L2',
  forward: true,
  recordDetail: true,
  extractUrlsFromResponse: true,
  neutralizeVueRouter: true,
  observe: { storageReads: false, cookieReads: false, xhrHeaders: true },
  neutralize: { fields: ['response_code', 'code'], success: 0 },
  stubs: [ /* 同 config.json stubs */ ],
  apiPattern: /\/(api|apis|v\d+|dev|internal|graphql)\//i,
};
```

검증: `window.__API_RECON_PRELOAD__ === true`이고 pathname이 안정적으로 유지되어야 합니다.

기록 결과 내보내기:

```javascript
JSON.stringify({
  apis: [...window.__API_RECON_LOG__],
  detail: window.__API_RECON_DETAIL__,
  routes: [...(window.__API_RECON_ROUTES__ || [])],
  observe: window.__API_RECON_OBSERVE__,
}, null, 2)
```

---

## D. preload / runtime Hook 기능

preload(coverage)와 runtime_harvest(depth)에 내장된 브라우저 Hook 기능 및 적용 범위:

| Hook 기능 | API 발견에 주는 정보 | 지원 |
|---|---|---|
| fetch / XHR.open 후킹 | 요청 URL/메서드 기록 | ✅ `recordDetail` + `__API_RECON_LOG__` |
| XHR.setRequestHeader 후킹 | Authorization 등의 헤더 발견 | ✅ `observe.xhrHeaders` |
| localStorage/cookie 읽기 후킹 | 세션 키 이름 확인 | ⚠️ 선택적 `observe.storageReads/cookieReads` |
| Vue 라우트 가져오기 | frontendRoutes 보완 | ✅ `__API_RECON_ROUTES__`(로드된 라우트) |
| Vue 라우트 가드 무력화 / 로그인 이동 차단 | 모듈을 활성화하여 API 발생 | ✅ `neutralizeVueRouter` + 네이티브 이동 무력화 |
| React 라우트 가져오기 | 라우트 보완 | ⚠️ 정적 분석 + 클릭, 전용 Hook 없음 |
| 페이지 이동 차단(로그인 path) | 페이지에 머물며 분석 | ⚠️ 업무 탐색을 막지 않도록 로그인 path만 차단 |
| 암호화 라이브러리 후킹(CryptoJS/SM 등) | 암호화 매개변수 → 평문 API body | ❌ 암호화 함수 입력을 직접 후킹해야 함, 결론은 config에 기록 |
| 안티 디버깅 우회 | 우회하지 않으면 runtime에서 API를 기록하지 못함 | ❌ 직접 처리 필요, 정적 분석은 계속 사용 가능 |

---

## E. Endpoint 추출 정규식(정적 결과가 너무 적을 때)

`harvest_static.py`의 `extract_endpoints`를 넓히거나 수동으로 처리합니다.

```bash
grep -rhoaE '"/[a-z][A-Za-z0-9_/\-]{3,}"' js | sort -u
grep -rhoaE '/api/[a-zA-Z0-9_./-]+' js | sort -u
```

---

## F. 문제 해결

| 현상 | 원인 → 처리 |
|---|---|
| 정적 API가 매우 적음 | endpoint 문법이 맞지 않음 → 정규식 확대(D절) |
| chunk 수 ≪ manifest | CSS-only 또는 배포되지 않은 chunk, 404는 이미 재시도됨 |
| runtime에 로그인 화면이 계속 표시됨 | 렌더링 관문 오류 → A1의 키명, 컨테이너, 인코딩, domain 재확인 |
| 기본 화면에는 진입했지만 모듈이 비어 있음 | 콘텐츠 관문 → 메뉴 forge(A3), `routes` path도 확인 |
| 각 라우트에 bootstrap/locale만 있음 | 권한 코드 부족 → I절 권한 트리 복원, `role_permissions` + `permissions/all` 이중 stub 확인 |
| 사이드바 항목은 있으나 하위 페이지가 비어 있음 | tree의 intermediate 노드 누락 또는 code와 `userRouteAuth` 불일치 |
| 모든 API가 로그인으로 이동 | 인터셉터 관문 → `neutralize` 확인, 중첩 필드는 walk 로직 확장 필요 |
| WS 프레임 0 | 사용자 상호작용 후에만 subscribe 가능, `perRouteMs` 증가 |
| 응답 본문이 비어 있음 | `forward: true`일 때만 실제 응답 존재 |
| Chromium 없음 | chromium 설치 또는 `config.chromium` / `CHROMIUM` 설정 |
| Mock이 많아도 로그인으로 돌아감 | Hook 적용이 늦거나 `location.href` setter 누락 → document-start + preload |
| 목록이 전부 비어 있음 | L3 빈 배열은 정상, 탭/설정/상세 클릭 계속 |
| Redux action을 라우트로 오인 | get/set/change/clear/toggle/upload를 포함한 내부 path 제외 |
| Vue가 계속 로그인으로 이동 | preload가 document-start가 아님 → 삽입 시점 변경, 또는 `neutralizeVueRouter: false`이면 가드 수동 제거 |
| 응답에 URL이 있으나 log에 없음 | `extractUrlsFromResponse` 활성화 또는 `__API_RECON_DETAIL__`에서 수동 추출 |
| Authorization 헤더명 불명 | `observe.xhrHeaders` 활성화 또는 DevTools에서 요청 헤더 확인 |
| runtime이 매우 느림 / 시간 초과 | `waitUntil: domcontentloaded`로 변경, `routeTimeout` 감소, `networkidle2` 사용 금지 |
| 프록시 연결 실패 | `proxy` / 환경 변수 확인, Puppeteer와 curl의 프록시 포트 일치 |

---

## G. 보안이 강화된 대상

서버가 세션을 단계별 검증하는 경우(위조 불가 서명 cookie, 서버 렌더링되어 stub 불가한 메뉴) runtime은 기본 화면에서 멈출 수 있습니다. 예상 동작:

- **정적 분석만으로 endpoint 열거 가능** — 모듈 path가 코드 안에 있음
- 승인 범위에서 허용한다면 같은 harness를 **실제 세션**으로 실행: `forward: true`, neutralize 불필요, 실제 methods/params/responses 캡처

---

## H. 단일 작업 확인 목록

1. 승인 범위 확인
2. `scripts/harvest_static.py` **읽기** → 대상에 맞게 조정 → 실행 → `api_static.txt`, `routes.txt` 검토
3. **Phase 1b**: path 기준점 주변 확장 + 바인딩 계층 → `param_candidates.json`(J절)
4. A1/A2/A3 역분석 → 사이트 전용 `config.json` 작성
5. `runtime_harvest.js` / `preload.js`를 **읽고 조정한 뒤** 실행
6. `runtimeMode=depth`: `npm install` → 조정한 harvest 스크립트 실행
7. `runtimeMode=coverage/both`: document-start에 조정한 preload 삽입 → browser MCP 동적 열거 + **매개변수 트리거 행렬**
8. 모듈이 렌더링되지 않음 → **I절 권한 트리 복원** → stub 수정 → 재실행
9. 여러 매개변수 샘플 diff + 오류 역추론 → `params_merged.json`
10. 병합 → `site_map.json` + `api_merged.txt`, 확인 범위·누락·스크립트 변경점을 사실대로 표시

---

## I. 권한 트리 복원(Phase 4 심화)

단순한 `menus: [{ path, show: true }]` forge가 작동하지 않고 하위 모듈이 계속 mount되지 않을 때 사용합니다.

### I1. auth 모듈 찾기

```bash
grep -l 'userRouteAuth' js/*.js
grep -l 'routeMap\|routeLink' js/*.js
grep -rhoaE 'getResultTree|role_permissions|permissions/all' js | head
```

**권한 API path**, **응답 필드명**, **사용처 chunk 파일명**을 기록합니다.

### I2. routeMap 추출

```bash
python3 scripts/extract_route_map.py recon/js recon/
# 产出 recon/route_map.json
```

`[!] no routeMap pattern found`이면 `extract_route_map.py`의 정규식을 넓히거나 수동 grep합니다.

```bash
grep -rhoaE '([A-Z_][A-Z0-9_]*):\{name:"[^"]*",link:"/[^"]+"\}' js | head -20
```

### I3. 권한 트리 + stub 구성

```bash
python3 scripts/build_perm_tree.py recon/js recon/ --config recon/config.json
```

스크립트 로직:
1. `userRouteAuth={MONITOR:{url:...},...}` 파싱(webpack 별칭 `He=o.DASHBOARD` 포함)
2. `route_map.json`으로 alias → 실제 path 해석
3. code 접두사로 parent 추론(`MONITOR_ALERT` → `MONITOR`)
4. `permissions_tree.json`, `permissions_all_stub.json`, `role_permissions_stub.json` 출력
5. `--config`이면 `config.json`의 `stubs`에 자동 기록하고 `routes` 확장

**대상에 맞게 조정**(스크립트 상단):
- `DEFAULT_ROOTS`: 최상위 모듈 code 목록
- `DEFAULT_PREFIX_PARENT`: `PREFIX_` → parent 매핑
- `DEFAULT_EXTRA_PARENT`: 접두사 관계가 아닌 orphan 노드

### I4. stub 일관성 검증

```bash
# permissions 数量应 ≈ userRouteAuth 条目数
wc -l recon/perm_codes_all.txt
# routes 应覆盖 route_map 全部 link
python3 -c "import json; m=json.load(open('recon/route_map.json')); r=set(json.load(open('recon/config.json'))['routes']); print('missing', [v['link'] for v in m.values() if v['link'] not in r])"
```

### I5. runtime 재실행 및 비교

```bash
node recon/runtime_harvest.js recon/config.json
# 对比 forge 前后 runtime_api.json 条数；检查 /attack、/asset 等是否出现模块 API
```

| forge 전 | forge 후(성공) |
|---|---|
| 모든 라우트에서 같은 bootstrap 3–5개 | 라우트마다 다른 module API 발생 |
| `/api/locale/language`만 있음 | `/api/web/...` 모듈 endpoint 발생 |
| `routes.txt`의 라우트가 한 자릿수 | route_map에서 `routes` 80–110+개 확보 |

### I6. 계속 실패하는 경우

- **coverage 모드**: 사이드바 + 탭 클릭, 상호작용 후에야 권한 gating 요청이 발생할 수 있음
- **stub 필드**: 실제 API(curl + 실제 session)와 stub의 nesting 비교
- **추가 가드**: `hasPermission|checkRole|func.` 등 버튼 수준 검사를 grep하고 `role_permissions.permissions` 확장
- **정적 대체**: 모듈 API path는 `api_static.txt`에 남아 있으며 runtime은 METHOD/body만 보완. 매개변수는 `param_candidates.json` + 기존 기록 샘플 보존

---

## J. 매개변수 역분석(Phase 1b / 5b / 5c)

**방법론이며 범용 스크립트가 아닙니다.** path는 정규식으로 찾고, 매개변수는 기준점 주변 확장 + UI 바인딩 경로 + 여러 샘플 diff + 오류 역추론으로 찾습니다.

### J1. 기준점 주변 확장 — path로 요청 구성 객체 찾기

```bash
# 以 Phase 1 已知 path 为锚
grep -n '"/api/user/list"' js/*.js
grep -rhoaE '.{0,120}("/api[^"]+").{0,200}' js | head
grep -rhoaE '(params|data|body|payload)\s*:\s*\{' js | head
grep -rhoaE '(get|post|put|delete|patch)\([^,]+,\s*\{' js | head
```

### J2. 래퍼 계층과 전송 형태

```bash
# axios / 统一 request
grep -rhoaE '(axios|request)\.(get|post|put|delete|patch)\(' js | head
grep -rhoaE 'interceptors\.(request|response)' js | head

# GraphQL
grep -rhoaE '(query|mutation)\s+\w+|gql`|graphql\(' js | head
grep -rhoaE '\$[a-zA-Z_]+\s*:\s*(Int|String|Boolean|\[)' js | head

# FormData / multipart
grep -rhoaE 'FormData|\.append\(' js | head

# 路径参数
grep -rhoaE 'path:\s*"/[^"]*:[^"]+"' js | head
grep -rhoaE 'useParams|route\.params|\$route\.params' js | head
```

### J3. 검증 관문 — 필수 / 형식 / 열거값

```bash
grep -rhoaE '(required|message|pattern|enum|validator)\s*:' js | head
grep -rhoaE 'yup\.|zod\.|async-validator|Form\.Item|a-form-item|el-form-item' js | head
grep -rhoaE 'rules\s*:\s*\[|name:\s*["\'][a-zA-Z_]+["\']' js | head
grep -rhoaE 'label.*value|options\s*:\s*\[' js | head
```

### J4. 바인딩 계층 — 양식 → API

```bash
grep -rhoaE 'onFinish|handleSubmit|getFieldsValue|validateFields' js | head
grep -rhoaE '(pick|omit|transform|dayjs|moment)\(' js | head
```

runtime 보완: DevTools → Network → 요청 → **Initiator**(call stack)에서 `fetch`/`send` 호출자를 따라 요청 구성 함수를 찾습니다.

### J5. 암호화 매개변수

```bash
grep -rhoaE 'encrypt|decrypt|sign|CryptoJS|sm2|sm3|sm4|RSA|AES' js | head
```

**암호문에서 필드를 추측하지 마세요.** 암호화 함수 **입력**을 후킹하여 암호화 전 plaintext payload를 기록하고 `config.json` / `param_candidates.json`에 결론을 적습니다.

### J6. 매개변수 트리거 행렬(Phase 3 필수)

모듈마다 각 작업을 한 번씩 기록하고 요청 body/query를 diff합니다.

| 작업 | 확인 사항 |
|---|---|
| 목록 첫 화면 | 페이지 나눔 기본값 |
| 검색 | keyword, filters |
| 고급 필터 | optional 필드 |
| 새로 만들기/편집 | 전체 entity |
| 일괄/내보내기 | `ids[]`, `exportType` |
| 정렬/페이지 이동 | `sortField`, `order` |

`param_samples.json` 생성: `[{ "path", "method", "action": "search", "body", "query", "headers" }]`

### J7. 신뢰도 규칙

| 신뢰도 | 조건 |
|---|---|
| **높음** | 정적 callsite + runtime ≥2개 샘플이 일치 |
| **중간** | 정적 분석만 수행 또는 runtime 1회만 수행 |
| **낮음** | 응답/오류에서 역추론, 추가 검증 없음 |
| **트리거 대기** | 정적 분석에서 필드는 확인했으나 UI/권한에 아직 도달하지 못함 |

### J8. 상황별 빠른 설정

| 상황 | 순서 |
|---|---|
| REST 목록 페이지 | J1 요청 구성 객체 → J6 네 번 diff → J3 rules |
| 새로 만들기/편집 양식 | J3 Form name → J4 submit 경로 → runtime 제출 + 일부러 비워서 400 확인 |
| GraphQL | J2 variables 선언 → runtime에서 각 operation의 variables 기록 |
| 암호화 body | J5 입력 후킹 → 암호화 전 필드가 실제 params |

### J9. api-recon 단계와 대응

| api-recon | 매개변수 recon |
|---|---|
| Phase 1 정적 분석 | J1 기준점 주변 확장 |
| Phase 2 A2 인터셉터 | 전역 삽입 필드(tenantId, sign) |
| Phase 3 runtime | J6 트리거 행렬 + `param_samples.json` |
| Phase 4 권한 트리 | 모듈별 양식이 다르므로 권한이 충분해야 전체 필드 발생 |
| Phase 5 병합 | `params_merged.json` + 신뢰도, 단일 샘플로 필수 여부를 판단하지 않음 |

### J10. 문제 해결

| 현상 | 처리 |
|---|---|
| 정적 분석의 필드명이 runtime에서 전혀 나타나지 않음 | 「트리거 대기」 표시, 권한 트리 보완 / 고급 필터 클릭 / 연동 select의 각 option 선택 |
| 같은 path에 서로 다른 body 형태 | 정상. `action`별로 나누어 기록하며 schema를 강제로 병합하지 않음 |
| stub 응답은 가짜지만 params를 확인하려는 경우 | **outbound 요청**의 body/headers를 확인하고 stub 응답에서 역추론하지 않음 |
| 400이 nested field를 지적 | 외부 래퍼 `data`/`bizData`/`variables` 확인 |
| GraphQL에서 operation 이름만 보임 | `variables` JSON을 펼치고 정적 분석에서 `$var: Type` 검색 |

---
