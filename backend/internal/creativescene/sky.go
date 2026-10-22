package creativescene

import (
	"os"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"
	_ "time/tzdata" // the same zone data on every machine, whatever the server has installed
	"unicode/utf8"
)

// SkyPhysical is the physical sky (W2): the sun from a place, a local date
// and time, or from manual angles, lighting the scene through a physical
// atmosphere. The renderer works it all out; the server only checks the
// fields and fills the ones left out.
const SkyPhysical = "physical"

const maxPlaceRunes = 80

var (
	datePattern      = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	localTimePattern = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)
)

// physicalOnly names the fields set on s that only a physical sky has.
func (s Sky) physicalOnly() []string {
	var names []string
	for _, f := range []struct {
		name string
		set  bool
	}{
		{"sun_mode", s.SunMode != nil}, {"latitude_deg", s.LatitudeDeg != nil}, {"longitude_deg", s.LongitudeDeg != nil},
		{"timezone", s.Timezone != nil}, {"place", s.Place != nil}, {"date", s.Date != nil}, {"local_time", s.LocalTime != nil},
		{"north_azimuth_deg", s.NorthAzimuthDeg != nil}, {"altitude_m", s.AltitudeM != nil}, {"air_density", s.AirDensity != nil},
		{"dust_density", s.DustDensity != nil}, {"ozone_density", s.OzoneDensity != nil}, {"sun_disc", s.SunDisc != nil},
		{"sun_size_deg", s.SunSizeDeg != nil}, {"sun_strength", s.SunStrength != nil}, {"sun_temperature_k", s.SunTemperatureK != nil},
	} {
		if f.set {
			names = append(names, f.name)
		}
	}
	return names
}

// completeSky fills what a physical sky leaves out with the contract's
// defaults: Shanghai at noon under a clear sky. The place comes whole or not
// at all, and the date has no default: set_environment replaces the sky, so
// a default would reset the date whenever it was left out.
func completeSky(s *Sky) error {
	if s.Kind != SkyPhysical {
		return nil
	}
	if n := countSet(s.LatitudeDeg != nil, s.LongitudeDeg != nil, s.Timezone != nil); n != 0 && n != 3 {
		return invalid("物理天空的 latitude_deg、longitude_deg、timezone 要么都写，要么都省略（都省略时为上海）")
	}
	fill(&s.SunMode, "location")
	fill(&s.LatitudeDeg, 31.2304)
	fill(&s.LongitudeDeg, 121.4737)
	fill(&s.Timezone, "Asia/Shanghai")
	fill(&s.Place, "")
	fill(&s.LocalTime, "12:00")
	fill(&s.NorthAzimuthDeg, 180.0)
	fill(&s.SunElevationDeg, 35.0)
	fill(&s.SunAzimuthDeg, 135.0)
	fill(&s.AltitudeM, 0.0)
	fill(&s.AirDensity, 1.0)
	fill(&s.DustDensity, 1.0)
	fill(&s.OzoneDensity, 1.0)
	fill(&s.SunDisc, true)
	fill(&s.SunSizeDeg, 0.545)
	fill(&s.SunStrength, 1.0)
	fill(&s.SunTemperatureK, 6500.0)
	return nil
}

func countSet(set ...bool) int {
	n := 0
	for _, s := range set {
		if s {
			n++
		}
	}
	return n
}

// validatePhysicalSky checks a completed physical sky against the contract,
// naming the field it finds wrong.
func validatePhysicalSky(s Sky) error {
	var other []string
	if s.Turbidity != nil {
		other = append(other, "turbidity")
	}
	if s.HDRI != nil {
		other = append(other, "hdri")
	}
	if s.Color != nil {
		other = append(other, "color")
	}
	if len(other) > 0 {
		return invalid("物理天空不带 %s：天空只带自己类型的字段", strings.Join(other, "、"))
	}
	if s.SunMode == nil || (*s.SunMode != "location" && *s.SunMode != "manual") {
		return invalid("物理天空的 sun_mode 须为 location（按地点和时间推算太阳）或 manual（手动角度）")
	}
	if s.Date == nil {
		return invalid("物理天空须有 date：当地日期，写作 YYYY-MM-DD")
	}
	if !validDate(*s.Date) {
		return invalid("date 须为 1900-01-01…2100-12-31 之间存在的日期，写作 YYYY-MM-DD，现在是 %q", *s.Date)
	}
	if s.LocalTime == nil || !localTimePattern.MatchString(*s.LocalTime) {
		return invalid("local_time 须为当地时刻 HH:MM（00:00…23:59）")
	}
	if s.Timezone == nil || !validTimezone(*s.Timezone) {
		return invalid("timezone 须为 IANA 时区名，大小写与时区库一致，例如 Asia/Shanghai、America/New_York")
	}
	if s.Place == nil || utf8.RuneCountInString(*s.Place) > maxPlaceRunes {
		return invalid("place 不能超过 %d 个字", maxPlaceRunes)
	}
	if s.SunDisc == nil {
		return invalid("物理天空缺少 sun_disc")
	}
	for _, r := range []struct {
		name   string
		v      *float64
		lo, hi float64
	}{
		{"latitude_deg", s.LatitudeDeg, -90, 90},
		{"longitude_deg", s.LongitudeDeg, -180, 180},
		{"north_azimuth_deg", s.NorthAzimuthDeg, 0, 360},
		{"sun_elevation_deg", s.SunElevationDeg, -10, 90},
		{"sun_azimuth_deg", s.SunAzimuthDeg, 0, 360},
		{"altitude_m", s.AltitudeM, 0, 20000},
		{"air_density", s.AirDensity, 0, 10},
		{"dust_density", s.DustDensity, 0, 50},
		{"ozone_density", s.OzoneDensity, 0, 10},
		{"sun_size_deg", s.SunSizeDeg, 0.1, 10},
		{"sun_strength", s.SunStrength, 0, 10},
		{"sun_temperature_k", s.SunTemperatureK, 2000, 12000},
	} {
		if !between(r.v, r.lo, r.hi) {
			return invalid("%s 须在 %g…%g 之间", r.name, r.lo, r.hi)
		}
	}
	return nil
}

// validDate accepts a date that exists, from 1900 to 2100.
func validDate(s string) bool {
	if !datePattern.MatchString(s) {
		return false
	}
	d, err := time.Parse(time.DateOnly, s)
	return err == nil && s >= "1900-01-01" && s <= "2100-12-31" && d.Format(time.DateOnly) == s
}

// zoneChecks remembers validTimezone's answers: every write validates the
// whole scene, and LoadLocation reads the zone file each time.
var zoneChecks sync.Map

// validTimezone accepts an IANA zone name as the zone database spells it.
// "" and "Local" load too, as UTC and as the server's own zone.
func validTimezone(name string) bool {
	if ok, seen := zoneChecks.Load(name); seen {
		return ok.(bool)
	}
	ok := name != "" && name != "Local"
	if ok {
		_, err := time.LoadLocation(name)
		ok = err == nil && spelledAsStored(name)
	}
	zoneChecks.Store(name, ok)
	return ok
}

// zoneDirs are where Go looks for zone files before its embedded copy
// (time/zoneinfo_unix.go).
var zoneDirs = []string{"/usr/share/zoneinfo/", "/usr/share/lib/zoneinfo/", "/usr/lib/locale/TZ/", "/etc/zoneinfo/"}

// spelledAsStored reports whether name, which LoadLocation found, is spelled
// as the zone files are. On a case-insensitive file system (macOS)
// LoadLocation also finds asia/shanghai, which Linux and the embedded zone
// data refuse; the location's String() only echoes the name it was given.
// A name the file system does not hold at all came from the embedded data,
// whose lookup is case-sensitive already.
func spelledAsStored(name string) bool {
	for _, dir := range zoneDirs {
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		for part := range strings.SplitSeq(name, "/") {
			entries, err := os.ReadDir(dir)
			if err != nil {
				return true
			}
			exact, folded := false, false
			for _, e := range entries {
				exact = exact || e.Name() == part
				folded = folded || strings.EqualFold(e.Name(), part)
			}
			if !exact {
				return !folded
			}
			dir = path.Join(dir, part)
		}
		return true
	}
	return true
}
