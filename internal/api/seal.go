package api

type SealRequest struct {
	ClientKey string `json:"clientKey"`
	Proof     string `json:"proof,omitempty"`
}

type SealResponse struct {
	SessionID string `json:"sessionId"`
	ServerKey string `json:"serverKey"`
	Confirm   string `json:"confirm"`
}

func (t Transfer) WithSealedNames(seal func(string) (string, bool)) (payload any, ok bool) {
	t.Name, ok = seal(t.Name)
	if !ok {
		return nil, false
	}
	return t, true
}

func (f OfferedFile) WithSealedNames(seal func(string) (string, bool)) (payload any, ok bool) {
	f.Name, ok = seal(f.Name)
	if !ok {
		return nil, false
	}
	return f, true
}

func (c FileChange) WithSealedNames(seal func(string) (string, bool)) (payload any, ok bool) {
	c.File.Name, ok = seal(c.File.Name)
	if !ok {
		return nil, false
	}
	return c, true
}
