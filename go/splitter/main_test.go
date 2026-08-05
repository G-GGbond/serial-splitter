package splitter

import "testing"

func TestListPorts(t *testing.T) {
	ports := ListPorts()
	t.Logf("枚举到 %d 个串口", len(ports))
	for _, p := range ports {
		t.Logf("  %s | %s", p.Device, p.Description)
	}
	if len(ports) == 0 {
		t.Error("未枚举到任何串口")
	}
}

func TestCom0comMap(t *testing.T) {
	m := Com0comMap()
	t.Logf("com0com 端口 %d 个", len(m))
	for name, pi := range m {
		t.Logf("  %s -> %s%d", name, pi.Kind, pi.Idx)
	}
}
