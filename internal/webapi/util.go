package webapi

import "strconv"

func parseInt(s string, out *int) (int, error) {
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, err
	}
	*out = v
	return v, nil
}
