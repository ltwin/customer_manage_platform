package shootplanning

import (
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	shootplanningapi "github.com/samson/customer-manage-platform/backend/internal/shootplanning/httpcontract"
)

// 契约枚举与列白名单是两份清单，会各自漂移。handler 用生成的 Valid()、repository 用
// planListOrder，两边都 fail-closed（不认的值一律 400），所以漂移不会放行非法排序，
// 但会让某个已发布的排序键永远拿不到结果——这里逐值对齐把这种漂移变成编译或测试失败。
func TestPlanListSortMatchesGeneratedContract(t *testing.T) {
	t.Parallel()

	// 白名单 → 契约：全自动，多出来的键会被抓到。
	for _, value := range []PlanListSort{PlanListSortUpdatedDesc, PlanListSortUpdatedAsc, PlanListSortCreatedDesc} {
		if !shootplanningapi.ShootPlanListSort(value).Valid() {
			t.Fatalf("whitelist sort %q is not a member of the generated contract enum", value)
		}
	}
	// 契约 → 白名单：只覆盖这里列出的生成常量；契约新增枚举值时需要同步补一行，
	// 常量被改名或删除会直接编译失败。
	for _, value := range []shootplanningapi.ShootPlanListSort{
		shootplanningapi.UpdatedAtDesc,
		shootplanningapi.UpdatedAtAsc,
		shootplanningapi.CreatedAtDesc,
	} {
		if _, ok := planListOrder(PlanListSort(value)); !ok {
			t.Fatalf("contract sort %q has no column whitelist entry", value)
		}
	}
}

func TestPlanListOrderWhitelist(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		sort  PlanListSort
		want  []store.OrderBy
		valid bool
	}{
		{name: "empty falls back to newest updated", sort: "", want: []store.OrderBy{{Column: "updated_at", Desc: true}, {Column: "id", Desc: true}}, valid: true},
		{name: "updated desc", sort: PlanListSortUpdatedDesc, want: []store.OrderBy{{Column: "updated_at", Desc: true}, {Column: "id", Desc: true}}, valid: true},
		{name: "updated asc", sort: PlanListSortUpdatedAsc, want: []store.OrderBy{{Column: "updated_at"}, {Column: "id"}}, valid: true},
		{name: "created desc", sort: PlanListSortCreatedDesc, want: []store.OrderBy{{Column: "created_at", Desc: true}, {Column: "id", Desc: true}}, valid: true},
		{name: "unknown key rejected", sort: "title_asc"},
		// 排序键永远不能变成 SQL 片段：注入形状必须落在白名单之外。
		{name: "sql fragment rejected", sort: "updated_at DESC, account_id"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := planListOrder(tt.sort)
			if ok != tt.valid {
				t.Fatalf("planListOrder(%q) ok = %v, want %v", tt.sort, ok, tt.valid)
			}
			if !tt.valid {
				if got != nil {
					t.Fatalf("rejected sort must not return columns, got %+v", got)
				}
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("planListOrder(%q) = %+v, want %+v", tt.sort, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("planListOrder(%q)[%d] = %+v, want %+v", tt.sort, i, got[i], tt.want[i])
				}
			}
		})
	}
}

// 每种排序都必须带 id 兜底，否则 updated_at/created_at 撞值时分页会漏行或重复行。
func TestPlanListOrderAlwaysBreaksTiesOnID(t *testing.T) {
	t.Parallel()

	for _, sort := range []PlanListSort{"", PlanListSortUpdatedDesc, PlanListSortUpdatedAsc, PlanListSortCreatedDesc} {
		order, ok := planListOrder(sort)
		if !ok {
			t.Fatalf("planListOrder(%q) unexpectedly rejected", sort)
		}
		last := order[len(order)-1]
		if last.Column != "id" {
			t.Fatalf("planListOrder(%q) last column = %q, want id", sort, last.Column)
		}
	}
}

func TestPlanListKeyword(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		raw    string
		want   string
		wantOK bool
	}{
		{name: "plain keyword", raw: "夜景", want: "夜景", wantOK: true},
		{name: "surrounding blanks are not part of the match", raw: "  夜景 \n", want: "夜景", wantOK: true},
		{name: "blank only counts as absent", raw: "   ", want: "", wantOK: true},
		{name: "empty stays empty", raw: "", want: "", wantOK: true},
		{name: "at the rune limit", raw: strings.Repeat("夜", PlanListQMaxRunes), want: strings.Repeat("夜", PlanListQMaxRunes), wantOK: true},
		// 长度按 rune 计：多字节中文不能因为字节数超限被误拒。
		{name: "over the rune limit", raw: strings.Repeat("夜", PlanListQMaxRunes+1), want: strings.Repeat("夜", PlanListQMaxRunes+1), wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := PlanListKeyword(tt.raw)
			if got != tt.want || ok != tt.wantOK {
				t.Fatalf("PlanListKeyword(%q) = (%q, %v), want (%q, %v)", tt.raw, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
