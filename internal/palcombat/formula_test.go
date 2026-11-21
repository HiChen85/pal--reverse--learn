package palcombat

import "testing"

func TestBaseDamageGolden(t *testing.T) {
	for _, tc := range []struct {
		attack, defense uint16
		want            int16
	}{{100, 50, 120}, {40, 50, 10}, {30, 50, 0}, {0, 0, 0}, {65535, 0, -2}, {33, 18, 37}, {35, 32, 19}} {
		if got := BaseDamage(tc.attack, tc.defense); got != tc.want {
			t.Errorf("BaseDamage(%d,%d)=%d want %d", tc.attack, tc.defense, got, tc.want)
		}
	}
	if PhysicalDamage(100, 50, 2) != 60 || PhysicalDamage(100, 50, 0) != 120 {
		t.Fatal("physical resistance must be a divisor with zero meaning no division")
	}
}
