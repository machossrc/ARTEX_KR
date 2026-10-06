# `/btw` 검증 기록

> 2026-09-10 원본 검증 기록을 한국어로 번역한 문서입니다. 아래 결과·경로·모델 이름은 원본 작성 당시의 기록이며, 이번 한국어화 작업에서 해당 모델이나 브라우저 검증을 새로 실행했다는 뜻이 아닙니다. 현재 검증은 루트 LOCALIZATION.md에 별도로 기록합니다.

날짜: 2026-09-10. 브랜치: `codex/btw-side-question`. 기준 커밋: `8dae851b9b622f2ff2631f332fde9719d0b16fba`.

독립 PostgreSQL 테스트 DB와 데이터 디렉터리를 사용했습니다. 실제 모델 자격 증명은 독립 테스트 환경에만 주입했고 코드나 이 기록에 저장하지 않았으며 제품 기본 모델도 바꾸지 않았습니다. 당시 환경은 Go 1.26.3, norma v0.3.6, Next.js 16.2.9입니다.

실제 모델 대화, 반환 객체, 엔지니어링 단언 및 Qwen 원본 심사 텍스트는 [validation-2026-09-10.json](validation-2026-09-10.json)에 보존되어 있으며 API 자격 증명은 포함하지 않습니다.

## 엔지니어링 검사

| 범위 | 결과 | 증거 |
| --- | --- | --- |
| 구조화 메시지와 도구 매개변수 깊은 복사 | 통과 | `TestCheckpointDeepCopyAndBoundaries` |
| 요약·압축 요청의 덮어쓰기 방지, 완전한 응답·최종 상태 게시, 부분 응답 제외 | 통과 | `TestCheckpointDeepCopyAndBoundaries`, `TestSnapshotExcludesPartialStreamAndSelectsPoolMember` |
| 실제 모델 풀 구성원 식별 | 통과 | `TestSnapshotExcludesPartialStreamAndSelectsPoolMember` |
| 도구 쌍, 20쌍 재생, 예산 축소 및 초과 오류 | 통과 | `TestBuildRequestCompactionToolPairingAndBudget` |
| 메인·보조 병렬 실행과 양방향 취소 격리 | 통과 | 차단형 Provider, `TestMainSideConcurrencyAndIndependentCancellation` |
| 도구 실행 없음, 스트리밍·비스트리밍 및 실패 시 확보한 사용량 | 통과 | `TestServiceNoToolsAndUsageOnFailure` |
| 실제 norma ChatAgent와 로컬 Read 도구, 메인 transcript·활동 격리 | 통과 | `TestSideActualChatCheckpointToolResultAndTranscriptIsolation`, 스트리밍·비스트리밍 하위 사례 |
| 영구 저장, 페이지 나눔, 멱등성, 재시작 시 부분 응답 보존 | 통과 | `TestSideHistoryIdempotencyPagingAndRecovery` |
| 초기화와 지연 쓰기 경합, 부모 자원 삭제, 버전 비교 | 통과 | `TestSideClearLateWritersAndDeletedParent` |
| MainAgent / Worker 보관·복원, v1/v2/v3 | 통과 | `TestSideTaskArchiveVersions` |
| 세 부모 인터페이스, 인증, 자원 귀속, Worker 논리 삭제 | 통과 | `TestSideHTTPGlobalLimitTaskWorkerAndDeletion`, `TestSideCheckpointPersistsBeforeAdmissionAndRestart` |
| 실행 중인 메인 세션의 보조 질문, 독립 SSE 재연결·해제, 취소·초기화 | 통과 | `TestSideHTTPBusyIsolationClearAndReconnect` |
| 부모 세션별 1개 / 전역 4개 동시 실행 | 통과 | 두 `TestSideHTTP…` 사례 |
| 제출 전 스냅샷 저장, 재시작 후 후속 질문, 이전 세션의 위조 스냅샷 금지 | 통과 | `TestSideCheckpointPersistsBeforeAdmissionAndRestart` |
| 캐시의 설정 삭제·모델 변경 후 계속 실행 거부 | 통과 | `TestSideRejectsDeletedOrChangedCachedProfile` |
| 보관 전 취소 후 최종 답변·사용량 저장 대기 | 통과 | `TestSideTaskDrainPersistsBeforeArchive` |
| 스트리밍 소비자 조기 취소 시 사용량 한 번 기록 및 보조 요청 귀속 | 통과 | `TestSideUsageRecordedOnceOnConsumerCancellation` |
| 재시작 자동 복원 Worker / deadline 컨텍스트가 새 스냅샷 계속 게시 | 통과 | `TestSideRestoredWorkerRuntimePublishesNewCheckpoint` |
| 관련 패키지 race 검사 | 통과 | 아래 명령 |
| TypeScript와 프로덕션 빌드 | 통과 | `npx tsc --noEmit`, `npm run build` |
| 새 프런트엔드 모듈 Biome | 통과 | `biome check`, 새 모듈 3개 |

별도의 폐기 가능한 DB를 `ARTEX_PG_DSN`으로 지정하여 자동화 검사를 재현할 수 있습니다. 운영 DB를 지정하지 마세요.

```sh
go test -race ./agent ./db ./server ./sidequestion ./llmrec ./llmpool \
  -run 'Test(Side|Checkpoint|Snapshot|BuildRequest|Service|MainSide|CaptureRun|TaskArchive|CompleteForwards|StopIntent|CancelIntent)' -count=1
cd web
npx tsc --noEmit
npx biome check src/lib/side-questions.ts src/hooks/use-side-questions.ts src/components/side-question-workspace.tsx
npm run build
```

당시 전체 Go 회귀는 모두 통과하지 않았습니다. `server` 패키지의 기존 테스트 두 개가 임시 디렉터리 정리 중 `TempDir RemoveAll … directory not empty`로 실패했습니다.

- `TestInheritedActivityDetailAndRelationDeletion`
- `TestTaskMetadataPatchReturnsRenameAndPin`

수정하지 않은 기준 커밋의 소스를 내보내 같은 격리 환경에서 `server` 패키지를 재실행했을 때도 두 정리 실패가 재현되었습니다. 기준 실행에서는 `TestCoreTaskLifecyclePG`의 목표 노드 수 단언도 실패했으나 최종 수정 후 `server` 회귀에서는 그 단언 실패가 없었습니다. 다른 패키지와 보조 질문 관련 사례·race 검사는 통과했습니다. 기준 코드의 문제를 이번 검증 통과로 표시하지 않았고 문제를 숨기려고 기존 단언을 수정하지도 않았습니다.

Next.js 빌드에는 기존의 다중 lockfile / workspace root 추론 경고가 있었지만 빌드와 모든 페이지 생성은 성공했습니다.

## 브라우저 검사

Codex In-app Browser를 독립 로컬 Go 서비스 및 Next.js 개발 서버에 연결했습니다. 데스크톱과 390 × 844 화면에서 다음 자동화·수동 동작을 수행하고 스크린샷과 브라우저 로그를 확인했습니다.

- 일반 대화 실행 중 `/btw` 입력 시 메인 내용과 보조 내용을 동시에 표시하며 데스크톱 사이드바가 정상 동작했습니다.
- 연속 후속 질문을 수행하고 보조 답변 중지 후 생성된 부분을 보존했으며 메인 흐름은 계속했습니다.
- 패널을 닫아도 요청이 계속되었고 다시 열면 완료된 답변을 복원했습니다. 새로 고침 후 빈 `/btw`로 기록을 복원했습니다.
- 좁은 화면 Drawer의 입력·버튼·기록·닫기가 정상이며 가로 넘침이 없었습니다.
- 초기화 확인 대화상자 후 기록은 지워지고 메인 transcript와 스냅샷은 보존되었습니다.
- 작업 MainAgent와 두 Worker에서 각각 질문하고 전환했을 때 에이전트 레이블과 기록이 섞이지 않았습니다.
- 차단형 로컬 모델 테스트 데이터로 Worker 실행을 유지했습니다. Worker 메인 입력창에서 `/btw`를 제출하고 보조 답변을 중지해도 Worker는 실시간 실행과 자체 일시 중지 버튼을 계속 표시했으며 보조 답변의 생성 부분은 저장되었습니다.
- 브라우저 오류·경고 로그는 비어 있었습니다.

통제된 테스트 데이터로 실제 모델의 출력 속도에 의존하지 않고 동시 실행 순서를 검증했습니다. 디버깅 중 두 번의 Worker 검사는 유효한 동시 실행 구간이 없었습니다(작업 종료 / 답변 조기 종료). 테스트 데이터를 수정한 뒤 다시 수행하여 통과했으며 처음의 동작은 유효한 통과로 기록하지 않았습니다.

## 실제 모델 대화

먼저 `grok-4.6`을 OpenAI 호환 인터페이스 `http://127.0.0.1:12580/tingly/openai`에서 확인했습니다. HTTP 200, 모델 이름 `grok-4.6`, `READY`가 반환되었고 2.82초가 걸렸습니다. 우선 모델이 사용 가능하여 Tingly `glm` 또는 Zhipu `glm-5.3` 대체 체인은 활성화하지 않았으며 이 두 서비스는 당시 검증하지 않았습니다.

| 상황 | 실제 결과 |
| --- | --- |
| 메인 세션 실행 중 자산·목표·표식 질문 | `redhaze.top`, 첫 페이지 읽기·요약 목표, `BTW-REAL-0910` 반환. 보조 답변 완료, 16.97초 |
| 메인 세션의 첫 페이지 읽기 완료 후 도구 근거 질문 | WebFetch 200, curl 301 → 302 → 200 이동, 페이지 제목을 정확히 인용. 7.24초 |
| 보조 질문에서 Bash로 테스트 파일 생성 요구 | 실행 거부, 대상 파일 미생성. 7.74초 |
| 완료된 보조 답변이 메인 컨텍스트를 변경하지 않음 | 메인 transcript SHA-256과 메인 활동 기록 동일, 보조 도구 실행 0회 |
| Go 서비스를 실제 중지·재시작한 뒤 후속 질문 | 이전 보조 기록 3건 보존, 저장된 스냅샷으로 자산·표식·제목 답변, 메인 에이전트 재실행 없음 |
| 새 세션에서 Grok 비스트리밍 설정 | 자산과 `ATOMIC-0910`을 정확히 답변. 사용량 input 11734, output 138, cache_read 11520 반환·저장 |

자산 사례의 메인 세션은 WebFetch와 Bash/curl로 공개 첫 페이지를 읽었습니다. 도착 페이지는 `https://id.redhaze.top/home`이고 실제 제목은 “红幕科技 RedHaze Group · 全球综合集团门户”였습니다. Bash는 응답을 로컬 테스트 파일에 임시 저장했으며 원격 쓰기는 수행하지 않았습니다. 이 사실과 「보조 질문이 도구를 실행하지 않음」은 별도로 검증했습니다.

메인 transcript 검증값: `e7e61f135a4a120954b539f357e8c4205d7d5cd7460dcaf3dc0fd066463e1d00`.

**사용량 한계:** Tingly의 Grok 스트리밍 응답에는 usage가 없었습니다. 별도로 `stream_options.include_usage=true`를 직접 전송하여 확인한 결과 HTTP 200, 데이터 프레임 12개, usage 프레임 0개였습니다. 따라서 스트리밍 테스트의 0은 엔드포인트가 사용량을 제공하지 않았다는 뜻이지 과금이 없었다는 뜻이 아닙니다. 비스트리밍 사용량과 테스트 데이터의 실패·취소 사용량은 정확히 저장되었습니다.

## Qwen 심사

심사 모델은 `qwen-flash`, OpenAI 호환 인터페이스는 `https://dashscope.aliyuncs.com/compatible-mode/v1`이며 HTTP 200이었습니다. 처음 세 실제 보조 대화, 메인 세션의 도구 근거 및 엔지니어링 단언을 제공했습니다. `verdict: accept`, `concerns: []`를 반환하여 답변이 자산·표식·페이지 읽기 증거와 일치하고 보조 도구 거부가 제약에 부합한다고 판단했습니다. 사용량은 prompt 6625, completion 312, total 6937이었습니다.

이 Qwen 심사에는 나중에 추가한 서비스 재시작 및 비스트리밍 테스트가 포함되지 않았습니다. Qwen의 「쓰기가 없음」이라는 요약은 지나치게 넓었습니다. 메인 세션의 curl은 로컬 응답 임시 파일을 실제 생성했으며 위에 명시했습니다. 동시 실행, 도구 실행 0회 및 transcript 격리는 엔지니어링 단언으로 판단하고 모델 심사는 답변 품질의 보조 평가에만 사용했습니다.
