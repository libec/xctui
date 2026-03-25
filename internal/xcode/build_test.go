package xcode

import (
	"slices"
	"strings"
	"sync"
	"testing"
)

func TestScanPipelines(t *testing.T) {
	tests := []struct {
		name        string
		inputString string
		prefix      string
		want        []string
	}{
		{
			"no prefix",
			"line one\nline two\n",
			"",
			[]string{"line one", "line two"},
		},
		{
			"foo prefix",
			"line one\nline two\n",
			"foo: ",
			[]string{"foo: line one", "foo: line two"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := strings.NewReader(tt.inputString)
			ch := make(chan string, 10)
			var wg sync.WaitGroup
			wg.Add(1)
			scanPipe(r, ch, tt.prefix, &wg)
			wg.Wait()
			close(ch)

			var got []string
			for line := range ch {
				got = append(got, line)
			}

			if !slices.Equal(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
