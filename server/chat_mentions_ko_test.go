package server

import "testing"

func TestKoreanChatMentionKindsAndLegacyAliases(t *testing.T) {
    names := []string{"취약점", "자산", "기업", "인터페이스", "IP", "앱", "도메인", "하위 도메인", "서비스"}
    kinds := []string{"finding", "asset", "company", "endpoint", "ip", "app", "root_domain", "subdomain", "service"}
    for i, name := range names {
        refs, err := parseChatMentions("@[" + name + "#42 한글 라벨]")
        if err != nil || len(refs) != 1 || refs[0].Kind != kinds[i] || refs[0].ID != 42 {
            t.Fatalf("%s: refs=%+v err=%v", name, refs, err)
        }
    }
    refs, err := parseChatMentions("@[漏洞#42 old] @[취약점#42 새 라벨] @[자산#43 example]")
    if err != nil || len(refs) != 2 { t.Fatalf("legacy deduplication: refs=%+v err=%v", refs, err) }
}
