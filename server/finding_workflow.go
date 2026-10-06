package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/traffic"
	actool "github.com/Autumn-27/norma/tool"
)

func (s *Server) seedFindingWorkflowTools() {
	const hostSearchDescriptionFlag = "finding_workflow_tools_v3_host_search_description"
	if value, _, _ := s.m.pg.GetSetting(hostSearchDescriptionFlag); value != "true" {
		// Only replace the original built-in text. A user-edited description is
		// authoritative and must survive upgrades.
		legacy := "기록 프록시가 수집한 대상 트래픽을 조회합니다(host 필수, URL 부분 문자열이나 본문 키워드로 추가 필터 가능). body_contains는 수집된 요청/응답 헤더와 본문에서 전체 텍스트를 검색하며 임의의 부분 문자열과 중국어를 지원합니다(최소 3자). 응답 속 비밀번호, 키, 오류, 내부망 주소 등을 찾을 수 있습니다. 응답 내용 없이 매우 가벼운 인덱스(id/method/url/status/resp_len)만 반환합니다. 기본 3건, 페이지당 최대 10건이며 결과가 많으면 page로 페이지를 이동합니다(page=0부터). 요청/응답 원문은 traffic_get(id)로 읽으세요. 방문한 자원이나 엔드포인트를 다시 찾을 때 먼저 사용하여 동일 URL에 curl을 반복하지 마세요."
		if _, err := s.m.pg.Exec(`UPDATE tools SET description=$1,updated_at=now() WHERE key='traffic_search' AND system AND description=$2`, traffic.TrafficSearchDescription, legacy); err != nil {
			// Log and leave the flag unset so the next startup retries; do not
			// return, or a transient error here would also skip the reporter
			// migration below — the two are independent.
			log.Printf("[evidence] upgrade traffic_search description: %v", err)
		} else {
			_ = s.m.pg.SetSetting(hostSearchDescriptionFlag, "true")
		}
	}
	const flag = "finding_workflow_tools_v2_reporter"
	if value, _, _ := s.m.pg.GetSetting(flag); value == "true" {
		return
	}
	for _, key := range []string{"report_finding", "add_hint", "add_task_hint"} {
		row, err := s.m.pg.GetTool(key)
		if err != nil {
			log.Printf("[evidence] load %s: %v", key, err)
			return
		}
		if row == nil || !row.System {
			continue
		}
		var schema map[string]any
		if err := json.Unmarshal(row.Schema, &schema); err != nil {
			log.Printf("[evidence] invalid schema for %s: %v", key, err)
			return
		}
		if schema == nil {
			log.Printf("[evidence] missing object schema for %s", key)
			return
		}
		props := objectProperty(schema, "properties")
		if key == "report_finding" {
			if _, exists := props["evidence_hint_id"]; !exists {
				props["evidence_hint_id"] = map[string]any{"type": "integer", "description": "선택: 이 작업에서 해당 취약점에 대응하는 hint ID. 힌트에 저장된 traffic_refs를 읽어 함께 연결합니다. 힌트가 없으면 생략하세요"}
			}
		} else {
			if _, exists := props["traffic_refs"]; !exists {
				props["traffic_refs"] = agent.HintTrafficSchema()
			}
			hints := objectProperty(props, "hints")
			if _, exists := hints["type"]; !exists {
				hints["type"] = "array"
			}
			items := objectProperty(hints, "items")
			if _, ok := items["type"]; !ok {
				items["type"] = "object"
			}
			itemProps := objectProperty(items, "properties")
			for name, value := range map[string]any{"text": strParam("힌트 내용"), "asset_ids": map[string]any{"type": "array", "items": map[string]any{"type": "integer"}}, "traffic_refs": agent.HintTrafficSchema()} {
				if _, exists := itemProps[name]; !exists {
					itemProps[name] = value
				}
			}
		}
		raw, _ := json.Marshal(schema)
		result, err := s.m.pg.Exec(`UPDATE tools SET schema=$2::jsonb,updated_at=now() WHERE key=$1 AND system AND schema=$3::jsonb`, key, string(raw), string(row.Schema))
		if err != nil {
			log.Printf("[evidence] upgrade %s: %v", key, err)
			return
		}
		if n, _ := result.RowsAffected(); n != 1 {
			return
		} // preserve concurrent user edits
	}
	// Upgrade only the original default binding. Customized lists and enabled
	// flags survive; the one-time flag also preserves future user unbinding.
	readers := `["worker","reporter"]`
	for _, key := range []string{"traffic_search", "traffic_get", "traffic_blob"} {
		if _, err := s.m.pg.Exec(`UPDATE tools SET agents=$2::jsonb WHERE key=$1 AND system AND (agents='["worker"]'::jsonb OR (agents @> '["worker","planner","mainagent","auto","pentest"]'::jsonb AND jsonb_array_length(agents)=5))`, key, readers); err != nil {
			return
		}
	}
	if _, err := s.m.pg.Exec(`UPDATE tools SET agents=$1::jsonb WHERE key='get_finding_traffic' AND system AND agents @> '["auto","reporter"]'::jsonb AND jsonb_array_length(agents)=2`, `["auto","reporter","worker","planner","mainagent","pentest"]`); err != nil {
		return
	}
	// Replace the previous code default only; preserve customized binding lists.
	if _, err := s.m.pg.Exec(`UPDATE tools SET agents='["reporter"]'::jsonb WHERE key='bind_finding_traffic' AND system AND agents @> '["worker","planner","mainagent","auto","pentest"]'::jsonb AND jsonb_array_length(agents)=5`); err != nil {
		return
	}
	if err := s.m.pg.AddAgentToToolBinding("reporter", []string{"bind_finding_traffic"}); err != nil {
		return
	}
	_ = s.m.pg.SetSetting(flag, "true")
}

func objectProperty(parent map[string]any, key string) map[string]any {
	value, ok := parent[key].(map[string]any)
	if !ok {
		value = map[string]any{}
		parent[key] = value
	}
	return value
}

func (s *Server) agentFindingTrafficAccess(ctx context.Context, id int64, write bool) error {
	if id <= 0 {
		return errors.New("finding_id는 독립 취약점 기록 ID여야 하며 탐색 노드 ID가 아닙니다")
	}
	f, err := s.m.pg.GetFinding(id)
	if err != nil {
		return err
	}
	if f == nil {
		return fmt.Errorf("%w: finding_id=%d. 증거 도구는 독립 취약점 기록 ID를 사용합니다. list_task_findings / get_task_node_detail의 finding_id 필드에서 읽으세요. id / finding_node_id를 전달하지 마세요", db.ErrFindingNotFound, id)
	}
	if ri := agent.RunInfoFrom(ctx); ri.TaskID > 0 {
		task := s.m.ResolveTask(strconv.FormatInt(ri.TaskID, 10))
		if task == nil {
			return errors.New("작업이 존재하지 않습니다")
		}
		_, inherited, allowed := findingProvenanceInTask(task, f.TaskID)
		if !allowed {
			return errors.New("현재 작업은 이 취약점을 읽을 수 없습니다")
		}
		if write && inherited {
			return errors.New("상속한 취약점의 트래픽 증거는 읽기 전용입니다. 원본 작업에서 수정하세요")
		}
	}
	return nil
}

func (s *Server) toolBindFindingTraffic() actool.CoreTool {
	return wrTool("bind_finding_traffic", "등록된 취약점에 검증한 실제 HTTP 트래픽을 추가 연결합니다. finding_id는 독립 취약점 기록 ID이며 탐색 노드 ID를 전달하지 마세요. 한 묶음의 인용은 모두 성공하거나 모두 실패하며 중복 인용은 기존 설명을 덮어쓰지 않습니다. 추가 연결 시 기존 보고서는 갱신 필요로 표시됩니다. 패킷을 채우려고 다시 탐지하거나 취약점을 중복 생성하지 마세요.",
		objSchema(map[string]any{"finding_id": strParam("독립 취약점 기록 ID. list_task_findings / get_task_node_detail의 finding_id 필드에서 읽습니다"), "traffic_refs": agent.HintTrafficSchema()}, "finding_id", "traffic_refs"),
		func(ctx context.Context, raw json.RawMessage) (actool.Result, error) {
			if !s.m.pg.GetBool(settingAgentTrafficBinding, false) {
				return actool.Errorf("에이전트의 트래픽 자동 연결이 꺼져 있습니다. 시스템 설정에서 켜거나 화면에서 수동으로 연결하세요."), nil
			}
			var args struct {
				FindingID json.RawMessage `json:"finding_id"`
				Refs      []db.TrafficRef `json:"traffic_refs"`
			}
			if err := json.Unmarshal(raw, &args); err != nil {
				return actool.Errorf(err.Error()), nil
			}
			id := parseProfileID(args.FindingID)
			if err := s.agentFindingTrafficAccess(ctx, id, true); err != nil {
				return actool.Errorf(err.Error()), nil
			}
			if len(args.Refs) == 0 {
				return actool.Errorf("추가 연결에는 검증한 traffic_refs가 하나 이상 필요합니다. 트래픽이 없으면 이 도구를 호출할 필요가 없습니다"), nil
			}
			list, err := s.evidenceStore().Bind(ctx, id, args.Refs)
			if err != nil {
				return actool.Errorf(err.Error()), nil
			}
			return jsonResult(trafficSummary(list))
		})
}
