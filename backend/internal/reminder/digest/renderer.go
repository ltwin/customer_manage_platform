package digest

import (
	"fmt"
	"strings"
	"time"
)

const maxDigestCodePoints = 3500

type Renderer struct{}

func NewRenderer() Renderer { return Renderer{} }

func (Renderer) Render(snapshot DigestSnapshot) string {
	lines := []string{fmt.Sprintf("%d月%d日经营摘要", snapshot.LocalDate.Month(), snapshot.LocalDate.Day()), ""}
	if snapshot.Reminders.Total == 0 && len(snapshot.TodayShootSlots) == 0 && snapshot.UnpaidCount == 0 {
		lines = append(lines, "今日暂无待处理事项")
		return truncateDigest(strings.Join(lines, "\n"))
	}

	lines = append(lines, fmt.Sprintf("提醒（%d）", snapshot.Reminders.Total))
	for _, reminder := range snapshot.Reminders.Items {
		label := "今日"
		if reminder.DueDate.Before(snapshot.LocalDate) {
			label = "逾期 " + reminder.DueDate.Format("01-02")
		}
		lines = append(lines, fmt.Sprintf("- [%s] %s", label, reminder.Content))
	}
	if overflow := snapshot.Reminders.Total - len(snapshot.Reminders.Items); overflow > 0 {
		lines = append(lines, fmt.Sprintf("- 另有 %d 条，请到网页查看", overflow))
	}
	if snapshot.Reminders.Total == 0 {
		lines = append(lines, "- 今日暂无提醒")
	}

	lines = append(lines, "", fmt.Sprintf("今日拍摄（%d）", len(snapshot.TodayShootSlots)))
	location, err := time.LoadLocation(snapshot.Timezone)
	if err != nil {
		location = time.UTC
	}
	for _, slot := range snapshot.TodayShootSlots {
		detail := slot.CustomerName
		if slot.PackageName != "" {
			detail += " · " + slot.PackageName
		}
		lines = append(lines, fmt.Sprintf("- %s–%s %s",
			slot.StartAt.In(location).Format("15:04"),
			slot.EndAt.In(location).Format("15:04"),
			detail,
		))
	}
	if len(snapshot.TodayShootSlots) == 0 {
		lines = append(lines, "- 今日暂无拍摄")
	}
	lines = append(lines, "", fmt.Sprintf("待收尾款：%d 笔", snapshot.UnpaidCount))
	return truncateDigest(strings.Join(lines, "\n"))
}

func truncateDigest(text string) string {
	runes := []rune(text)
	if len(runes) <= maxDigestCodePoints {
		return text
	}
	marker := []rune("\n内容已截断，请到网页查看")
	return string(runes[:maxDigestCodePoints-len(marker)]) + string(marker)
}
