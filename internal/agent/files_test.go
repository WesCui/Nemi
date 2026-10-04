package agent

import (
	"strings"
	"testing"

	"nemi/internal/domain"
	"nemi/internal/files"
)

func TestExactTableAggregationAndSafeExports(t *testing.T) {
	group := 0
	value, err := aggregate(domain.Table{Name: "账单", Rows: [][]string{{"类型", "金额"}, {"餐饮", "0.1"}, {"餐饮", "0.2"}, {"餐饮", "不明"}, {"交通", "-2.5"}}}, 1, true, &group)
	if err != nil {
		t.Fatal(err)
	}
	groups := value.(map[string]any)["groups"].([]map[string]any)
	if groups[1]["sum"] != "0.30000000" || groups[1]["skipped"] != 1 || groups[0]["sum"] != "-2.50000000" {
		t.Fatal(value)
	}
	data, c, err := artifact("结果.csv", "列,值\n文本,=HYPERLINK(\"https://bad.example\")\n金额,-2.5\n")
	// The URL cell contains quotes and must be properly quoted CSV.
	if err == nil {
		t.Fatal("malformed CSV accepted", c)
	}
	data, c, err = artifact("结果.csv", "列,值\n文本,=1+1\n金额,-2.5\n")
	if err != nil || !strings.Contains(string(data), "'=1+1") || c.Tables[0].Rows[2][1] != "-2.5" {
		t.Fatal(err, c)
	}
	data, c, err = artifact("结果.xlsx", "列,值\n文本,=1+1\n金额,0.3\n")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := files.Parse("结果.xlsx", data)
	if err != nil || parsed.Tables[0].Rows[1][1] != "=1+1" {
		t.Fatal(err, parsed)
	}
	if _, _, err = artifact("运行.html", "<script>x</script>"); err == nil {
		t.Fatal("active artifact accepted")
	}
}
