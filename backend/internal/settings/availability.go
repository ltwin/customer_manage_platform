package settings

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
)

const (
	DefaultMinOpeningMinutes = 120
	DefaultTurnaroundMinutes = 60
)

var localTimePattern = regexp.MustCompile(`^(?:[01][0-9]|2[0-3]):[0-5][0-9]$`)

// ScheduleAvailabilityWindow 是账号时区内单日的一段可约本地时间。
type ScheduleAvailabilityWindow struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

// ScheduleAvailabilityWeekly 使用完整 ISO weekday 映射；nil 表示当天不可约。
type ScheduleAvailabilityWeekly struct {
	Monday    *ScheduleAvailabilityWindow `json:"1"`
	Tuesday   *ScheduleAvailabilityWindow `json:"2"`
	Wednesday *ScheduleAvailabilityWindow `json:"3"`
	Thursday  *ScheduleAvailabilityWindow `json:"4"`
	Friday    *ScheduleAvailabilityWindow `json:"5"`
	Saturday  *ScheduleAvailabilityWindow `json:"6"`
	Sunday    *ScheduleAvailabilityWindow `json:"7"`
}

// ScheduleAvailability 是账号级可约偏好；timezone 继续由 Settings 单独提供。
type ScheduleAvailability struct {
	Weekly            ScheduleAvailabilityWeekly `json:"weekly"`
	MinOpeningMinutes int                        `json:"min_opening_minutes"`
	TurnaroundMinutes int                        `json:"turnaround_minutes"`
}

// DefaultScheduleAvailability 返回完整七日默认值，调用方可安全修改各日窗口。
func DefaultScheduleAvailability() ScheduleAvailability {
	return ScheduleAvailability{
		Weekly: ScheduleAvailabilityWeekly{
			Monday:    newAvailabilityWindow("10:00", "19:00"),
			Tuesday:   newAvailabilityWindow("10:00", "19:00"),
			Wednesday: newAvailabilityWindow("10:00", "19:00"),
			Thursday:  newAvailabilityWindow("10:00", "19:00"),
			Friday:    newAvailabilityWindow("10:00", "19:00"),
			Saturday:  newAvailabilityWindow("09:00", "20:00"),
			Sunday:    newAvailabilityWindow("09:00", "20:00"),
		},
		MinOpeningMinutes: DefaultMinOpeningMinutes,
		TurnaroundMinutes: DefaultTurnaroundMinutes,
	}
}

func newAvailabilityWindow(start, end string) *ScheduleAvailabilityWindow {
	return &ScheduleAvailabilityWindow{Start: start, End: end}
}

// ValidateScheduleAvailability 校验运行时领域约束；nil weekday 是合法的停用日。
func ValidateScheduleAvailability(value ScheduleAvailability) error {
	if value.MinOpeningMinutes < 15 || value.MinOpeningMinutes > 480 {
		return ValidationError{Message: "availability.min_opening_minutes 须在 15-480"}
	}
	if value.TurnaroundMinutes < 0 || value.TurnaroundMinutes > 240 {
		return ValidationError{Message: "availability.turnaround_minutes 须在 0-240"}
	}
	windows := []*ScheduleAvailabilityWindow{
		value.Weekly.Monday,
		value.Weekly.Tuesday,
		value.Weekly.Wednesday,
		value.Weekly.Thursday,
		value.Weekly.Friday,
		value.Weekly.Saturday,
		value.Weekly.Sunday,
	}
	for index, window := range windows {
		if window == nil {
			continue
		}
		if !localTimePattern.MatchString(window.Start) || !localTimePattern.MatchString(window.End) {
			return ValidationError{Message: fmt.Sprintf("availability.weekly.%d 时间须为 HH:MM", index+1)}
		}
		if window.End <= window.Start {
			return ValidationError{Message: fmt.Sprintf("availability.weekly.%d.end 须晚于 start", index+1)}
		}
	}
	return nil
}

// DecodeScheduleAvailabilityJSON 严格解码 availability：拒绝缺失/额外星期和未知嵌套字段。
func DecodeScheduleAvailabilityJSON(data []byte) (ScheduleAvailability, error) {
	var root struct {
		Weekly            json.RawMessage `json:"weekly"`
		MinOpeningMinutes *int            `json:"min_opening_minutes"`
		TurnaroundMinutes *int            `json:"turnaround_minutes"`
	}
	if err := decodeStrictJSON(data, &root); err != nil {
		return ScheduleAvailability{}, ValidationError{Message: "availability 格式错误: " + err.Error()}
	}
	if len(root.Weekly) == 0 || root.MinOpeningMinutes == nil || root.TurnaroundMinutes == nil {
		return ScheduleAvailability{}, ValidationError{Message: "availability 缺少必填字段"}
	}

	var rawWeekly map[string]json.RawMessage
	if err := json.Unmarshal(root.Weekly, &rawWeekly); err != nil || rawWeekly == nil {
		return ScheduleAvailability{}, ValidationError{Message: "availability.weekly 须为对象"}
	}
	allowed := map[string]struct{}{"1": {}, "2": {}, "3": {}, "4": {}, "5": {}, "6": {}, "7": {}}
	for key := range rawWeekly {
		if _, ok := allowed[key]; !ok {
			return ScheduleAvailability{}, ValidationError{Message: "availability.weekly 含额外星期 " + key}
		}
	}
	for key := range allowed {
		if _, ok := rawWeekly[key]; !ok {
			return ScheduleAvailability{}, ValidationError{Message: "availability.weekly 缺少星期 " + key}
		}
	}

	weekly := ScheduleAvailabilityWeekly{}
	destinations := map[string]**ScheduleAvailabilityWindow{
		"1": &weekly.Monday,
		"2": &weekly.Tuesday,
		"3": &weekly.Wednesday,
		"4": &weekly.Thursday,
		"5": &weekly.Friday,
		"6": &weekly.Saturday,
		"7": &weekly.Sunday,
	}
	for key, destination := range destinations {
		window, err := decodeAvailabilityWindow(rawWeekly[key], key)
		if err != nil {
			return ScheduleAvailability{}, err
		}
		*destination = window
	}
	value := ScheduleAvailability{
		Weekly:            weekly,
		MinOpeningMinutes: *root.MinOpeningMinutes,
		TurnaroundMinutes: *root.TurnaroundMinutes,
	}
	if err := ValidateScheduleAvailability(value); err != nil {
		return ScheduleAvailability{}, err
	}
	return value, nil
}

func decodeAvailabilityWindow(data []byte, weekday string) (*ScheduleAvailabilityWindow, error) {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return nil, nil
	}
	var raw struct {
		Start *string `json:"start"`
		End   *string `json:"end"`
	}
	if err := decodeStrictJSON(data, &raw); err != nil {
		return nil, ValidationError{Message: "availability.weekly." + weekday + " 格式错误: " + err.Error()}
	}
	if raw.Start == nil || raw.End == nil {
		return nil, ValidationError{Message: "availability.weekly." + weekday + " 缺少 start 或 end"}
	}
	return &ScheduleAvailabilityWindow{Start: *raw.Start, End: *raw.End}, nil
}

func decodeStrictJSON(data []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("包含多个 JSON 值")
		}
		return err
	}
	return nil
}

func encodeScheduleAvailabilityJSON(value ScheduleAvailability) ([]byte, error) {
	if err := ValidateScheduleAvailability(value); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func isZeroScheduleAvailability(value ScheduleAvailability) bool {
	return value.MinOpeningMinutes == 0 && value.TurnaroundMinutes == 0 &&
		value.Weekly.Monday == nil && value.Weekly.Tuesday == nil && value.Weekly.Wednesday == nil &&
		value.Weekly.Thursday == nil && value.Weekly.Friday == nil && value.Weekly.Saturday == nil &&
		value.Weekly.Sunday == nil
}
