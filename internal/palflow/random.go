package palflow

import "fmt"

type ScriptRandomSource interface {
	RandomLong(min, max int) (int, error)
}
type ScriptRandomProvider interface{ ScriptRandom() ScriptRandomSource }

func consumeRandom(state *State, inputs []Input, from, to int, reason string) (int, *InputUse, error) {
	if state == nil || state.InputIndex < 0 || state.InputIndex > len(inputs) {
		return 0, nil, fmt.Errorf("random input cursor is outside the supplied input stream")
	}
	if to <= from {
		return from, nil, nil
	}
	span := int64(to) - int64(from) + 1
	if span <= 0 || span > 2147483647 {
		return 0, nil, fmt.Errorf("random interval is outside 32-bit reference range")
	}
	maxActual := int64(from) + 2147483647/(2147483647/span)
	generated := (state.Random != nil || state.World != nil && state.World.RandomMode == "seeded") && (state.InputIndex >= len(inputs) || inputs[state.InputIndex].Kind != InputRandom)
	index := state.InputIndex
	var value int
	if generated {
		var err error
		if state.Random != nil {
			value, err = state.Random.RandomLong(from, to)
		} else {
			state.World.BattleRandomState += 0x9e3779b97f4a7c15
			x := state.World.BattleRandomState
			x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
			x = (x ^ (x >> 27)) * 0x94d049bb133111eb
			x ^= x >> 31
			value = from + int(x%uint64(span))
		}
		if err != nil {
			return 0, nil, err
		}
		index = -1
	} else {
		if index >= len(inputs) {
			return 0, nil, fmt.Errorf("explicit random input is required")
		}
		if inputs[index].Kind != InputRandom {
			return 0, nil, fmt.Errorf("input %d: expected random, received %q", index, inputs[index].Kind)
		}
		value = inputs[index].Value
	}
	if int64(value) < int64(from) || int64(value) > maxActual {
		return 0, nil, fmt.Errorf("input %d: invalid random value %d for %s (%d..%d, reference division tail through %d)", index, value, reason, from, to, maxActual)
	}
	if !generated {
		state.InputIndex++
	}
	return value, &InputUse{Index: index, Generated: generated, Input: Input{Kind: InputRandom, Value: value}, Meaning: fmt.Sprintf("%s: RandomLong(%d,%d), actual reference upper bound %d", reason, from, to, maxActual)}, nil
}
