package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/authevent"
)

func TestAuthMonitorThresholdBoundaries(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name         string
		args         []string
		events       []authevent.Event
		wantExit     int
		wantCode     string
		wantSeverity string
	}{
		{name: "normal event", args: []string{"auth", "monitor"}, events: []authevent.Event{successfulLoginEvent(base)}, wantExit: monitorExitClean},
		{name: "any refresh reuse", args: []string{"auth", "monitor"}, events: []authevent.Event{failedEvent(base, authevent.RefreshReuse, "reuse")}, wantExit: monitorExitAlert, wantCode: "refresh_reuse", wantSeverity: "high"},
		{name: "rate global below", args: []string{"auth", "monitor"}, events: rateEvents(base, 19, false), wantExit: monitorExitClean},
		{name: "rate global at", args: []string{"auth", "monitor"}, events: rateEvents(base, 20, false), wantExit: monitorExitAlert, wantCode: "rate_limited_global", wantSeverity: "warning"},
		{name: "rate source below", args: []string{"auth", "monitor"}, events: rateEvents(base, 4, true), wantExit: monitorExitClean},
		{name: "rate source at", args: []string{"auth", "monitor"}, events: rateEvents(base, 5, true), wantExit: monitorExitAlert, wantCode: "rate_limited_source", wantSeverity: "warning"},
		{name: "mail consecutive below", args: []string{"auth", "monitor"}, events: mailEvents(base, 4, 0), wantExit: monitorExitClean},
		{name: "mail consecutive at", args: []string{"auth", "monitor"}, events: mailEvents(base, 5, 0), wantExit: monitorExitAlert, wantCode: "mail_consecutive_failures", wantSeverity: "warning"},
		{name: "mail rate exactly twenty percent", args: []string{"auth", "monitor"}, events: mailEvents(base, 2, 8), wantExit: monitorExitClean},
		{name: "mail rate above twenty percent", args: []string{"auth", "monitor"}, events: mailEvents(base, 3, 7), wantExit: monitorExitAlert, wantCode: "mail_failure_rate", wantSeverity: "warning"},
		{name: "legacy dry run ignored", args: []string{"auth", "monitor"}, events: []authevent.Event{legacyEvent(base, true)}, wantExit: monitorExitClean},
		{name: "legacy off warning", args: []string{"auth", "monitor", "--cutover-mode=off"}, events: []authevent.Event{legacyEvent(base, false)}, wantExit: monitorExitAlert, wantCode: "legacy_claim_failure", wantSeverity: "warning"},
		{name: "legacy active high", args: []string{"auth", "monitor", "--cutover-mode=active"}, events: []authevent.Event{legacyEvent(base, false)}, wantExit: monitorExitAlert, wantCode: "legacy_claim_failure", wantSeverity: "high"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := runMonitorFixture(t, test.args, eventLines(t, test.events))
			if code != test.wantExit {
				t.Fatalf("exit=%d want=%d stdout=%q stderr=%q", code, test.wantExit, stdout, stderr)
			}
			if test.wantCode == "" {
				if !strings.Contains(stdout, `"status":"clean"`) || stderr != "" {
					t.Fatalf("clean output: stdout=%q stderr=%q", stdout, stderr)
				}
				return
			}
			if !strings.Contains(stdout, `"code":"`+test.wantCode+`"`) ||
				!strings.Contains(stdout, `"severity":"`+test.wantSeverity+`"`) {
				t.Fatalf("missing alert: stdout=%q", stdout)
			}
		})
	}
}

func TestAuthMonitorExitMatrixAndMalformedLineIsolation(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	canary := "raw-canary-secret@example.invalid"
	tests := []struct {
		name       string
		input      string
		wantExit   int
		wantStatus string
	}{
		{name: "empty", input: "", wantExit: monitorExitClean, wantStatus: "clean"},
		{name: "alert", input: eventLines(t, []authevent.Event{failedEvent(base, authevent.RefreshReuse, "reuse")}), wantExit: monitorExitAlert, wantStatus: "alert"},
		{name: "degraded", input: canary + "\n", wantExit: monitorExitDegraded, wantStatus: "degraded"},
		{name: "alert and degraded continues", input: canary + "\n" + eventLines(t, []authevent.Event{failedEvent(base, authevent.RefreshReuse, "reuse")}), wantExit: monitorExitAlertDegraded, wantStatus: "alert_degraded"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := runMonitorFixture(t, []string{"auth", "monitor"}, test.input)
			if code != test.wantExit || !strings.Contains(stdout, `"status":"`+test.wantStatus+`"`) {
				t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
			if strings.Contains(stderr, canary) {
				t.Fatalf("stderr leaked malformed input: %q", stderr)
			}
			if test.wantExit >= monitorExitDegraded && !strings.Contains(stderr, "line=1 class=json_syntax") {
				t.Fatalf("missing fixed parse failure: %q", stderr)
			}
		})
	}
}

func TestAuthMonitorSupportsFileAndJournaldAdapters(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	eventLine := strings.TrimSpace(eventLines(t, []authevent.Event{failedEvent(base, authevent.RefreshReuse, "reuse")}))

	var stdout, stderr bytes.Buffer
	opened := ""
	code := run(context.Background(), []string{"auth", "monitor", "--file=fixture.jsonl"}, commandDeps{
		stdin:  strings.NewReader(""),
		stdout: &stdout,
		stderr: &stderr,
		openFile: func(path string) (io.ReadCloser, error) {
			opened = path
			return io.NopCloser(strings.NewReader(eventLine)), nil
		},
	})
	if code != monitorExitAlert || opened != "fixture.jsonl" {
		t.Fatalf("file adapter: exit=%d opened=%q stdout=%q stderr=%q", code, opened, stdout.String(), stderr.String())
	}

	journalLine, err := json.Marshal(map[string]any{"MESSAGE": eventLine, "_SYSTEMD_UNIT": "crm.service"})
	if err != nil {
		t.Fatal(err)
	}
	nonAuthJournalLine, err := json.Marshal(map[string]any{"MESSAGE": "service started", "_SYSTEMD_UNIT": "crm.service"})
	if err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"auth", "monitor", "--source=journald"}, commandDeps{
		stdin: strings.NewReader(string(nonAuthJournalLine) + "\n" + string(journalLine) + "\n"), stdout: &stdout, stderr: &stderr,
	})
	if code != monitorExitAlert || !strings.Contains(stdout.String(), `"code":"refresh_reuse"`) || stderr.Len() != 0 {
		t.Fatalf("journald adapter: exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestAuthMonitorRejectsUnknownEventFieldsAndReadFailuresWithoutEcho(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	line := strings.TrimSpace(eventLines(t, []authevent.Event{successfulLoginEvent(base)}))
	line = strings.TrimSuffix(line, "}") + `,"email":"raw-owner@example.invalid"}`
	stdout, stderr, code := runMonitorFixture(t, []string{"auth", "monitor"}, line+"\n")
	if code != monitorExitDegraded || !strings.Contains(stderr, "line=1 class=event_schema") ||
		strings.Contains(stderr, "raw-owner@example.invalid") || !strings.Contains(stdout, `"status":"degraded"`) {
		t.Fatalf("unknown field handling: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}

	var readStdout, readStderr bytes.Buffer
	code = run(context.Background(), []string{"auth", "monitor"}, commandDeps{
		stdin: errorReader{}, stdout: &readStdout, stderr: &readStderr,
	})
	if code != monitorExitDegraded || !strings.Contains(readStderr.String(), "class=input_read") ||
		strings.Contains(readStderr.String(), "synthetic reader detail") {
		t.Fatalf("read failure handling: exit=%d stdout=%q stderr=%q", code, readStdout.String(), readStderr.String())
	}
}

func runMonitorFixture(t *testing.T, args []string, input string) (string, string, int) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), args, commandDeps{
		stdin: strings.NewReader(input), stdout: &stdout, stderr: &stderr,
		openFile: func(string) (io.ReadCloser, error) { return nil, errors.New("unexpected file open") },
	})
	return stdout.String(), stderr.String(), code
}

func eventLines(t *testing.T, events []authevent.Event) string {
	t.Helper()
	var output strings.Builder
	for _, event := range events {
		encoded, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		output.Write(encoded)
		output.WriteByte('\n')
	}
	return output.String()
}

func successfulLoginEvent(at time.Time) authevent.Event {
	return authevent.Event{Timestamp: at, Name: authevent.Login, Result: authevent.ResultSuccess}
}

func failedEvent(at time.Time, name authevent.Name, class string) authevent.Event {
	return authevent.Event{Timestamp: at, Name: name, Result: authevent.ResultFailure, FailureClass: class}
}

func rateEvents(base time.Time, count int, sameSource bool) []authevent.Event {
	events := make([]authevent.Event, 0, count)
	for i := 0; i < count; i++ {
		source := monitorDigest(byte('a' + i))
		if sameSource {
			source = monitorDigest('s')
		}
		events = append(events, authevent.Event{
			Timestamp: base.Add(time.Duration(i) * time.Second), Name: authevent.RateLimited,
			Result: authevent.ResultFailure, FailureClass: "rate_limited", Action: "login", SourceDigest: source,
		})
	}
	return events
}

func mailEvents(base time.Time, failures, successes int) []authevent.Event {
	events := make([]authevent.Event, 0, failures+successes)
	for i := 0; i < failures; i++ {
		events = append(events, authevent.Event{
			Timestamp: base.Add(time.Duration(i) * time.Second), Name: authevent.MailDelivery,
			Result: authevent.ResultFailure, FailureClass: "temporarily_unavailable", Action: "password_reset",
		})
	}
	for i := 0; i < successes; i++ {
		events = append(events, authevent.Event{
			Timestamp: base.Add(time.Duration(failures+i) * time.Second), Name: authevent.MailDelivery,
			Result: authevent.ResultSuccess, Action: "password_reset",
		})
	}
	return events
}

func legacyEvent(at time.Time, dryRun bool) authevent.Event {
	return authevent.Event{
		Timestamp: at, Name: authevent.LegacyClaim, Result: authevent.ResultFailure,
		FailureClass: "not_ready", DryRun: &dryRun,
	}
}

func monitorDigest(char byte) string { return "v1:" + strings.Repeat(string(char), 43) }

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("synthetic reader detail") }
