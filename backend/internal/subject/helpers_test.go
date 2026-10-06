package subject

import "encoding/base64"

func base64URL(s string) string { return base64.URLEncoding.EncodeToString([]byte(s)) }
