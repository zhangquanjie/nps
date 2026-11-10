package bridge

import (
	"reflect"
	"testing"
)

func TestExpandTCPPorts(t *testing.T) {
	tests := []struct {
		spec   string
		ports  []int
		ranges []string
	}{
		{"21,56000-56002", []int{21, 56000, 56001, 56002}, []string{"", "56000-56002", "56000-56002", "56000-56002"}},
		{"30-31,22,10-11", []int{30, 31, 22, 10, 11}, []string{"30-31", "30-31", "", "10-11", "10-11"}},
		{"21,22", []int{21, 22}, []string{"", ""}},
		{"21,invalid,22-22", []int{21, 22}, []string{"", ""}},
	}
	for _, tt := range tests {
		t.Run(tt.spec, func(t *testing.T) {
			ports, ranges := expandTCPPorts(tt.spec)
			if !reflect.DeepEqual(ports, tt.ports) || !reflect.DeepEqual(ranges, tt.ranges) {
				t.Fatalf("expandTCPPorts(%q) = %v, %v; want %v, %v", tt.spec, ports, ranges, tt.ports, tt.ranges)
			}
		})
	}
}
