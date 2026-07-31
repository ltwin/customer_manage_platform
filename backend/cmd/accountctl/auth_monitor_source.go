package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/authevent"
)

const monitorMaxLineBytes = 1 << 20

type monitorParseError struct{ class string }

func (err monitorParseError) Error() string { return err.class }

func scanMonitorEvents(reader io.Reader, source string, stderr io.Writer) ([]authevent.Event, int) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), monitorMaxLineBytes)
	events := make([]authevent.Event, 0)
	degraded := 0
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		event, present, err := parseMonitorLine(scanner.Bytes(), source)
		if err != nil {
			degraded++
			writeMonitorDiagnostic(stderr, lineNumber, monitorParseClass(err))
			continue
		}
		if present {
			events = append(events, event)
		}
	}
	if scanner.Err() != nil {
		degraded++
		writeMonitorDiagnostic(stderr, lineNumber+1, "input_read")
	}
	return events, degraded
}

func writeMonitorDiagnostic(stderr io.Writer, lineNumber int, class string) {
	if _, err := fmt.Fprintf(stderr, "line=%d class=%s\n", lineNumber, class); err != nil {
		return
	}
}

func parseMonitorLine(line []byte, source string) (authevent.Event, bool, error) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return authevent.Event{}, false, nil
	}
	if source == monitorSourceJournald {
		var journal map[string]json.RawMessage
		if err := json.Unmarshal(line, &journal); err != nil {
			return authevent.Event{}, false, monitorParseError{class: "json_syntax"}
		}
		rawMessage, ok := journal["MESSAGE"]
		if !ok {
			return authevent.Event{}, false, nil
		}
		var message string
		if err := json.Unmarshal(rawMessage, &message); err != nil {
			return authevent.Event{}, false, monitorParseError{class: "journald_schema"}
		}
		line = []byte(message)
		if !json.Valid(bytes.TrimSpace(line)) {
			if strings.Contains(message, "auth.") {
				return authevent.Event{}, false, monitorParseError{class: "json_syntax"}
			}
			return authevent.Event{}, false, nil
		}
	}
	return decodeMonitorEvent(line)
}

func decodeMonitorEvent(line []byte) (authevent.Event, bool, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(line, &raw); err != nil {
		return authevent.Event{}, false, monitorParseError{class: "json_syntax"}
	}
	eventRaw, exists := raw["event"]
	if !exists {
		return authevent.Event{}, false, nil
	}
	var name authevent.Name
	if err := json.Unmarshal(eventRaw, &name); err != nil {
		return authevent.Event{}, false, monitorParseError{class: "event_schema"}
	}
	if !strings.HasPrefix(string(name), "auth.") {
		return authevent.Event{}, false, nil
	}
	if !authevent.Supported(name) {
		return authevent.Event{}, false, monitorParseError{class: "event_schema"}
	}
	allowed := map[string]bool{
		"timestamp": true, "time": true, "level": true, "msg": true,
		"event": true, "result": true, "failure_class": true,
		"account_ref": true, "session_ref": true, "family_ref": true,
		"action": true, "source_digest": true, "provider_message_id": true, "dry_run": true,
	}
	for key := range raw {
		if !allowed[key] {
			return authevent.Event{}, false, monitorParseError{class: "event_schema"}
		}
	}
	if raw["timestamp"] != nil && raw["time"] != nil {
		return authevent.Event{}, false, monitorParseError{class: "event_schema"}
	}
	var wire struct {
		Timestamp         string           `json:"timestamp"`
		Time              string           `json:"time"`
		Name              authevent.Name   `json:"event"`
		Result            authevent.Result `json:"result"`
		FailureClass      string           `json:"failure_class"`
		AccountRef        string           `json:"account_ref"`
		SessionRef        string           `json:"session_ref"`
		FamilyRef         string           `json:"family_ref"`
		Action            string           `json:"action"`
		SourceDigest      string           `json:"source_digest"`
		ProviderMessageID string           `json:"provider_message_id"`
		DryRun            *bool            `json:"dry_run"`
	}
	if err := json.Unmarshal(line, &wire); err != nil {
		return authevent.Event{}, false, monitorParseError{class: "event_schema"}
	}
	timestamp := wire.Timestamp
	if timestamp == "" {
		timestamp = wire.Time
	}
	at, err := time.Parse(time.RFC3339Nano, timestamp)
	if err != nil {
		return authevent.Event{}, false, monitorParseError{class: "event_schema"}
	}
	event := authevent.Event{
		Timestamp: at.UTC(), Name: wire.Name, Result: wire.Result, FailureClass: wire.FailureClass,
		AccountRef: wire.AccountRef, SessionRef: wire.SessionRef, FamilyRef: wire.FamilyRef,
		Action: wire.Action, SourceDigest: wire.SourceDigest,
		ProviderMessageID: wire.ProviderMessageID, DryRun: wire.DryRun,
	}
	if err := event.Validate(); err != nil {
		return authevent.Event{}, false, monitorParseError{class: "event_schema"}
	}
	return event, true, nil
}

func monitorParseClass(err error) string {
	if classified, ok := err.(monitorParseError); ok {
		return classified.class
	}
	return "event_schema"
}
