package errors

import (
	"runtime"
	"strconv"
	"strings"
)

// stackDepth is how many frames we capture at error-construction time. Enough
// to locate the source without inflating allocations.
const stackDepth = 16

// captureStack records caller program counters, skipping the constructor
// frames so the top frame is the user's callsite.
func captureStack(skip int) []uintptr {
	pcs := make([]uintptr, stackDepth)
	n := runtime.Callers(skip+1, pcs)
	return pcs[:n]
}

// formatStack renders the captured stack as a multi-line "func\n\tfile:line"
// string, matching Go's runtime debug format.
func formatStack(pcs []uintptr) string {
	if len(pcs) == 0 {
		return ""
	}
	frames := runtime.CallersFrames(pcs)
	var sb strings.Builder
	for {
		f, more := frames.Next()
		sb.WriteString(f.Function)
		sb.WriteString("\n\t")
		sb.WriteString(f.File)
		sb.WriteByte(':')
		sb.WriteString(strconv.Itoa(f.Line))
		sb.WriteByte('\n')
		if !more {
			break
		}
	}
	return sb.String()
}
