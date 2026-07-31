package main

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"sort"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/authevent"
)

const (
	monitorExitClean         = 0
	monitorExitAlert         = 1
	monitorExitDegraded      = 2
	monitorExitAlertDegraded = 3

	monitorSourceJSONL    = "jsonl"
	monitorSourceJournald = "journald"
	monitorCutoverOff     = "off"
	monitorCutoverActive  = "active"
)

type monitorAlert struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
}

type monitorReport struct {
	Operation     string         `json:"operation"`
	Status        string         `json:"status"`
	Events        int            `json:"events"`
	DegradedLines int            `json:"degraded_lines"`
	Alerts        []monitorAlert `json:"alerts"`
}

func runAuthMonitor(_ context.Context, args []string, deps commandDeps) int {
	flags := flag.NewFlagSet("accountctl-auth-monitor", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	cutoverMode := flags.String("cutover-mode", monitorCutoverOff, "")
	source := flags.String("source", monitorSourceJSONL, "")
	filePath := flags.String("file", "", "")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 ||
		(*cutoverMode != monitorCutoverOff && *cutoverMode != monitorCutoverActive) ||
		(*source != monitorSourceJSONL && *source != monitorSourceJournald) {
		return writeMonitorFailure(deps, "invalid_input")
	}

	reader := deps.stdin
	var closeReader func()
	if *filePath != "" {
		file, err := deps.openFile(*filePath)
		if err != nil {
			return writeMonitorFailure(deps, "input_read")
		}
		reader = file
		closeReader = func() { _ = file.Close() }
	}
	if closeReader != nil {
		defer closeReader()
	}

	events, degradedLines := scanMonitorEvents(reader, *source, deps.stderr)
	alerts := evaluateMonitorEvents(events, *cutoverMode)
	status, exitCode := monitorStatus(len(alerts) > 0, degradedLines > 0)
	report := monitorReport{
		Operation: "auth.monitor", Status: status, Events: len(events),
		DegradedLines: degradedLines, Alerts: alerts,
	}
	if err := json.NewEncoder(deps.stdout).Encode(report); err != nil {
		return monitorExitDegraded
	}
	return exitCode
}

func writeMonitorFailure(deps commandDeps, class string) int {
	writeMonitorDiagnostic(deps.stderr, 0, class)
	_ = json.NewEncoder(deps.stdout).Encode(monitorReport{
		Operation: "auth.monitor", Status: "degraded", Alerts: []monitorAlert{}, DegradedLines: 1,
	})
	return monitorExitDegraded
}

func monitorStatus(alert, degraded bool) (string, int) {
	switch {
	case alert && degraded:
		return "alert_degraded", monitorExitAlertDegraded
	case alert:
		return "alert", monitorExitAlert
	case degraded:
		return "degraded", monitorExitDegraded
	default:
		return "clean", monitorExitClean
	}
}

func evaluateMonitorEvents(events []authevent.Event, cutoverMode string) []monitorAlert {
	ordered := append([]authevent.Event(nil), events...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].Timestamp.Before(ordered[j].Timestamp)
	})
	alerts := make([]monitorAlert, 0, 6)
	seen := make(map[string]bool)
	add := func(code, severity string) {
		if seen[code] {
			return
		}
		seen[code] = true
		alerts = append(alerts, monitorAlert{Code: code, Severity: severity})
	}

	for _, event := range ordered {
		if event.Name == authevent.RefreshReuse {
			add("refresh_reuse", "high")
		}
		if event.Name == authevent.LegacyClaim && event.Result == authevent.ResultFailure &&
			event.DryRun != nil && !*event.DryRun {
			severity := "warning"
			if cutoverMode == monitorCutoverActive {
				severity = "high"
			}
			add("legacy_claim_failure", severity)
		}
	}
	evaluateRateLimitAlerts(ordered, add)
	evaluateMailAlerts(ordered, add)
	return alerts
}

func evaluateRateLimitAlerts(events []authevent.Event, add func(string, string)) {
	rateEvents := filterEvents(events, authevent.RateLimited)
	left := 0
	sourceCounts := make(map[string]int)
	for right, event := range rateEvents {
		sourceCounts[event.SourceDigest]++
		for event.Timestamp.Sub(rateEvents[left].Timestamp) > 5*time.Minute {
			sourceCounts[rateEvents[left].SourceDigest]--
			left++
		}
		if right-left+1 >= 20 {
			add("rate_limited_global", "warning")
		}
		if sourceCounts[event.SourceDigest] >= 5 {
			add("rate_limited_source", "warning")
		}
	}
}

func evaluateMailAlerts(events []authevent.Event, add func(string, string)) {
	mailEvents := filterEvents(events, authevent.MailDelivery)
	consecutiveFailures := 0
	left := 0
	windowFailures := 0
	for right, event := range mailEvents {
		if event.Result == authevent.ResultFailure {
			consecutiveFailures++
			windowFailures++
		} else {
			consecutiveFailures = 0
		}
		if consecutiveFailures >= 5 {
			add("mail_consecutive_failures", "warning")
		}
		for event.Timestamp.Sub(mailEvents[left].Timestamp) > 15*time.Minute {
			if mailEvents[left].Result == authevent.ResultFailure {
				windowFailures--
			}
			left++
		}
		samples := right - left + 1
		if samples >= 10 && windowFailures*100 > samples*20 {
			add("mail_failure_rate", "warning")
		}
	}
}

func filterEvents(events []authevent.Event, name authevent.Name) []authevent.Event {
	filtered := make([]authevent.Event, 0)
	for _, event := range events {
		if event.Name == name {
			filtered = append(filtered, event)
		}
	}
	return filtered
}
