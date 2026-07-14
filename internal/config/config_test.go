package config

import (
	"testing"
)

func TestParseTargets(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int
		wantErr bool
	}{
		{"단일 노드", "server-1=http://host:5000/api/diagnostics/stats", 1, false},
		{"다중 노드", "server-1=http://a:5000/x,server-2=http://b:5000/x", 2, false},
		{"빈 문자열", "", 0, false},
		{"잘못된 형식(= 없음)", "server-1", 0, true},
		{"빈 URL", "server-1=", 0, true},
		{"공백 포함", " server-1 = http://a:5000/x ", 1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTargets(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tt.wantErr)
			}
			if len(got) != tt.want {
				t.Fatalf("개수 = %d, want = %d", len(got), tt.want)
			}
		})
	}
}
