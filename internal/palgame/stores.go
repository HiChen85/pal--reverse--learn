package palgame

import "fmt"

func (s Source) StoreItems(id uint16) ([]uint16, error) {
	if s.Runtime == nil || int(id) >= len(s.Runtime.Stores) {
		return nil, fmt.Errorf("store %d outside imported DATA table", id)
	}
	stock := s.Runtime.Stores[id]
	return append([]uint16(nil), stock[:]...), nil
}
