// SPDX-License-Identifier: MIT

package engine

import (
	"github.com/otfabric/go-modbus-trace/export/mbcapexport"
	"github.com/otfabric/go-modbus-trace/mbtcp"
	"github.com/otfabric/go-modbus-trace/trace"
)

// quality grades a trace for the badge (R4): problems when results are
// incomplete (gaps, resyncs that failed, drops by the capture host, a
// damaged file, internal errors), warnings for conditions worth a look
// (retransmissions, reordering, conflicts, guessed roles, snap-length
// cuts), ok otherwise. The count is the number of conditions present.
func quality(s *trace.FileSummary) (string, int) {
	if s == nil {
		return "ok", 0
	}
	problems, warnings := 0, 0
	q := s.Quality
	for _, n := range []uint64{q.Gaps, q.ResyncFailed, s.InternalErrorCount} {
		if n > 0 {
			problems++
		}
	}
	if s.CaptureDrop != nil && *s.CaptureDrop > 0 {
		problems++
	}
	if s.Error != "" || s.Reader.TruncatedTail || s.Reader.SkippedBlocks > 0 {
		problems++
	}
	for _, n := range []uint64{q.Retransmitted, q.OutOfOrder, q.OverlapConflicts, q.Resyncs, q.SnaplenTruncated, q.ForeignFrames,
		s.TransactionFlags["role_guessed"], s.Transactions["orphan_response"], s.Transactions["ambiguous"], s.Transactions["unanswered"]} {
		if n > 0 {
			warnings++
		}
	}
	switch {
	case problems > 0:
		return "problems", problems + warnings
	case warnings > 0:
		return "warnings", warnings
	}
	return "ok", 0
}

// flagMeaning says in one sentence what a quality flag means (R6, Notes).
var flagMeaning = map[string]string{
	"retransmitted":            "Some of its bytes were seen more than once; the first copy was used.",
	"out_of_order":             "Its packets arrived out of order and were put back in order.",
	"gap_adjacent":             "It is next to bytes the capture missed; something may be missing before or after it.",
	"resynced":                 "Framing was lost before it and recovered here; it may not be a real ADU.",
	"snaplen_truncated":        "The capture cut the packet short (snap length); the ADU is incomplete.",
	"midstream":                "The capture started after the connection opened.",
	"role_guessed":             "Which side is the server was inferred from the traffic, not from the handshake.",
	"role_unknown":             "Which side is the server could not be decided.",
	"retried":                  "The client sent the same request again with the same transaction ID.",
	"clock_order":              "The response is stamped before its request (merged captures or a clock step).",
	"slow":                     "The response took longer than the slow threshold.",
	"invalid_pdu":              "A PDU breaks a rule of the Modbus specification (see the warnings).",
	"serial_function_over_tcp": "A function normally used on serial lines appeared over TCP.",
	"unknown_function":         "The function code is not one the decoder knows.",
	"broadcast":                "Unit 0 got no response, as expected for a broadcast.",
	"foreign_protocol":         "The MBAP protocol ID is not 0: this is not Modbus and was skipped by its length.",
	"other":                    "A condition this version of the application does not name.",
}

// warningMeaning says what a validation warning means.
var warningMeaning = map[string]string{
	mbtcp.WarnEmpty:     "The PDU is empty.",
	mbtcp.WarnTruncated: "The PDU is shorter than its function needs.",
}

func meaningOfWarning(code string) string {
	if m, ok := warningMeaning[code]; ok {
		return m
	}
	return "A rule of the Modbus specification is broken (" + code + ")."
}

// reasonText explains an export classification (MBCAP-EXPORT.md section 3).
func reasonText(r mbcapexport.Reason) string {
	switch r {
	case mbcapexport.Admit:
		return "Will be exported to MBCAP."
	case mbcapexport.WriteFunction:
		return "Not exported: writes are not part of replay data."
	case mbcapexport.FC23:
		return "Not exported: FC23 reads and writes at once; replay data holds reads only."
	case mbcapexport.UnsupportedFunction:
		return "Not exported: vendor and unknown functions are not replayed."
	case mbcapexport.FC08StateChanging:
		return "Not exported: this diagnostics sub-function changes the device's state."
	case mbcapexport.MEINot14:
		return "Not exported: only device identification (MEI type 14) is replayed."
	case mbcapexport.Unanswered:
		return "Not exported: no response was observed."
	case mbcapexport.OrphanResponse:
		return "Not exported: the response has no request."
	case mbcapexport.AmbiguousPair:
		return "Not exported: the response could belong to more than one request."
	case mbcapexport.Uncertain:
		return "Not exported: a gap or a resync makes it uncertain."
	case mbcapexport.Malformed:
		return "Not exported: the ADU is malformed."
	case mbcapexport.InvalidPDU, mbcapexport.ResponseMismatch:
		return "Not exported: the PDU breaks the specification or does not fit its request."
	case mbcapexport.SnaplenTruncated:
		return "Not exported: the capture cut it short."
	case mbcapexport.Resynced:
		return "Not exported: it follows a framing resync."
	case mbcapexport.ClockOrder:
		return "Not exported: the response is stamped before its request."
	case mbcapexport.RoleUnknown:
		return "Not exported: which side is the server is unknown."
	case mbcapexport.InvalidTime:
		return "Not exported: the response has no usable timestamp."
	case mbcapexport.ExceptionsDisabled, mbcapexport.NotSelected, mbcapexport.OutsideWindow:
		// Decided by an export's options, never by Classify: not stored.
	}
	return "Not exported (" + string(r) + ")."
}
