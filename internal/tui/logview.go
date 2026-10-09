package tui

// measure says how many rows of the window a line takes.
type measure func(line string) int

// logView is a scrollable window onto a growing list of lines, each taking
// rows(line) rows once wrapped. Only the lines in view get measured or
// rendered, so appending stays cheap however long a build log gets.
type logView struct {
	lines  []string
	rows   measure
	offset int // index of the first visible line
	height int
	follow bool // keep the newest line in view as lines arrive
}

func newLogView(rows measure) logView {
	return logView{rows: rows, follow: true}
}

func (v *logView) append(lines ...string) {
	v.lines = append(v.lines, lines...)
	if v.follow {
		v.offset = v.tail()
	}
}

func (v *logView) setHeight(height int) {
	v.height = height
	if v.follow {
		v.offset = v.tail()
	}
	v.scrollTo(v.offset)
}

// scroll moves by delta rows in whole lines: past every line that fits in delta
// rows together, and at least one.
func (v *logView) scroll(delta int) {
	i := v.offset
	switch {
	case delta > len(v.lines) || -delta > len(v.lines):
		i += delta // past an end whatever the wrapping: no line is shorter than a row
	case delta > 0:
		for used := 0; i < len(v.lines) && used+v.rows(v.lines[i]) <= delta; i++ {
			used += v.rows(v.lines[i])
		}
		i = max(i, v.offset+1)
	case delta < 0:
		for used := 0; i > 0 && used+v.rows(v.lines[i-1]) <= -delta; i-- {
			used += v.rows(v.lines[i-1])
		}
		i = min(i, v.offset-1)
	}
	v.scrollTo(i)
}

// scrollTo moves to offset. Reaching the bottom turns following back on.
func (v *logView) scrollTo(offset int) {
	tail := v.tail()
	v.offset = max(0, min(offset, tail))
	v.follow = v.offset == tail
}

// tail is the offset that shows the end of the log: the first of the last lines
// that fit the window together, or the last line alone when even it does not fit.
func (v logView) tail() int {
	i, used := len(v.lines), 0
	for i > 0 && used+v.rows(v.lines[i-1]) <= v.height {
		used += v.rows(v.lines[i-1])
		i--
	}
	if i == len(v.lines) {
		return max(0, i-1)
	}
	return i
}

// visible is the lines from offset that fill the window; the last may only partly fit.
func (v logView) visible() []string {
	i, used := v.offset, 0
	for i < len(v.lines) && used < v.height {
		used += v.rows(v.lines[i])
		i++
	}
	return v.lines[v.offset:i]
}
