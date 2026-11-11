package native

import (
	"fmt"
	"geblang/internal/runtime"
	nethttp "net/http"
	"strings"
	"time"
)

func HTTPCookieFromValue(value runtime.Value) (runtime.HTTPCookie, error) {
	switch value := value.(type) {
	case runtime.HTTPCookie:
		return value, nil
	case runtime.String:
		response := nethttp.Response{Header: nethttp.Header{}}
		response.Header.Add("Set-Cookie", value.Value)
		cookies := response.Cookies()
		if len(cookies) == 0 {
			return runtime.HTTPCookie{}, fmt.Errorf("invalid Set-Cookie header")
		}
		return httpCookieFromNative(cookies[0]), nil
	case runtime.Dict:
		cookie := runtime.HTTPCookie{}
		if name := dictString(value, "name"); name != "" {
			cookie.Name = name
		}
		if cookie.Name == "" {
			return cookie, fmt.Errorf("cookie name is required")
		}
		cookie.Value = dictString(value, "value")
		cookie.Path = dictString(value, "path")
		cookie.Domain = dictString(value, "domain")
		if maxAge, ok := dictInt64(value, "maxAge"); ok {
			cookie.MaxAge = maxAge
		}
		if expires, ok := dictInt64(value, "expires"); ok {
			cookie.Expires = expires
		}
		cookie.Secure = dictBool(value, "secure")
		cookie.HTTPOnly = dictBool(value, "httpOnly")
		cookie.SameSite = dictString(value, "sameSite")
		if err := validateSameSite(cookie.SameSite); err != nil {
			return cookie, err
		}
		return cookie, nil
	default:
		return runtime.HTTPCookie{}, fmt.Errorf("cookie must be dict, string, or http.Cookie")
	}
}

func HTTPCookieMethod(receiver runtime.HTTPCookie, name string, args []runtime.Value) (runtime.Value, error) {
	switch name {
	case "name":
		if len(args) != 0 {
			return nil, fmt.Errorf("http.Cookie.name expects no arguments")
		}
		return runtime.String{Value: receiver.Name}, nil
	case "value":
		if len(args) != 0 {
			return nil, fmt.Errorf("http.Cookie.value expects no arguments")
		}
		return runtime.String{Value: receiver.Value}, nil
	case "path":
		if len(args) != 0 {
			return nil, fmt.Errorf("http.Cookie.path expects no arguments")
		}
		return runtime.String{Value: receiver.Path}, nil
	case "domain":
		if len(args) != 0 {
			return nil, fmt.Errorf("http.Cookie.domain expects no arguments")
		}
		return runtime.String{Value: receiver.Domain}, nil
	case "maxAge":
		if len(args) != 0 {
			return nil, fmt.Errorf("http.Cookie.maxAge expects no arguments")
		}
		return runtime.NewInt64(receiver.MaxAge), nil
	case "expires":
		if len(args) != 0 {
			return nil, fmt.Errorf("http.Cookie.expires expects no arguments")
		}
		if receiver.Expires == 0 {
			return runtime.Null{}, nil
		}
		return runtime.NewInt64(receiver.Expires), nil
	case "secure":
		if len(args) != 0 {
			return nil, fmt.Errorf("http.Cookie.secure expects no arguments")
		}
		return runtime.Bool{Value: receiver.Secure}, nil
	case "httpOnly":
		if len(args) != 0 {
			return nil, fmt.Errorf("http.Cookie.httpOnly expects no arguments")
		}
		return runtime.Bool{Value: receiver.HTTPOnly}, nil
	case "sameSite":
		if len(args) != 0 {
			return nil, fmt.Errorf("http.Cookie.sameSite expects no arguments")
		}
		return runtime.String{Value: receiver.SameSite}, nil
	case "withValue":
		text, err := singleString(args, "http.Cookie.withValue")
		if err != nil {
			return nil, err
		}
		receiver.Value = text
		return receiver, nil
	case "withPath":
		text, err := singleString(args, "http.Cookie.withPath")
		if err != nil {
			return nil, err
		}
		receiver.Path = text
		return receiver, nil
	case "withDomain":
		text, err := singleString(args, "http.Cookie.withDomain")
		if err != nil {
			return nil, err
		}
		receiver.Domain = text
		return receiver, nil
	case "withMaxAge":
		seconds, err := singleInt64(args, "http.Cookie.withMaxAge")
		if err != nil {
			return nil, err
		}
		receiver.MaxAge = seconds
		return receiver, nil
	case "withExpires":
		seconds, err := singleInt64(args, "http.Cookie.withExpires")
		if err != nil {
			return nil, err
		}
		receiver.Expires = seconds
		return receiver, nil
	case "withSecure":
		value, err := singleBool(args, "http.Cookie.withSecure")
		if err != nil {
			return nil, err
		}
		receiver.Secure = value
		return receiver, nil
	case "withHttpOnly":
		value, err := singleBool(args, "http.Cookie.withHttpOnly")
		if err != nil {
			return nil, err
		}
		receiver.HTTPOnly = value
		return receiver, nil
	case "withSameSite":
		text, err := singleString(args, "http.Cookie.withSameSite")
		if err != nil {
			return nil, err
		}
		if err := validateSameSite(text); err != nil {
			return nil, err
		}
		receiver.SameSite = text
		return receiver, nil
	case "toHeader", "toString":
		if len(args) != 0 {
			return nil, fmt.Errorf("http.Cookie.%s expects no arguments", name)
		}
		return runtime.String{Value: nativeCookie(receiver).String()}, nil
	case "toDict":
		if len(args) != 0 {
			return nil, fmt.Errorf("http.Cookie.toDict expects no arguments")
		}
		return httpCookieToDict(receiver), nil
	default:
		return nil, fmt.Errorf("http.Cookie has no method %s", name)
	}
}

func httpCookieFromNative(cookie *nethttp.Cookie) runtime.HTTPCookie {
	out := runtime.HTTPCookie{
		Name:     cookie.Name,
		Value:    cookie.Value,
		Path:     cookie.Path,
		Domain:   cookie.Domain,
		MaxAge:   int64(cookie.MaxAge),
		Secure:   cookie.Secure,
		HTTPOnly: cookie.HttpOnly,
		SameSite: sameSiteString(cookie.SameSite),
	}
	if !cookie.Expires.IsZero() {
		out.Expires = cookie.Expires.Unix()
	}
	return out
}

func nativeCookie(cookie runtime.HTTPCookie) *nethttp.Cookie {
	out := &nethttp.Cookie{
		Name:     cookie.Name,
		Value:    cookie.Value,
		Path:     cookie.Path,
		Domain:   cookie.Domain,
		MaxAge:   int(cookie.MaxAge),
		Secure:   cookie.Secure,
		HttpOnly: cookie.HTTPOnly,
		SameSite: nativeSameSite(cookie.SameSite),
	}
	if cookie.Expires != 0 {
		out.Expires = time.Unix(cookie.Expires, 0).UTC()
	}
	return out
}

func httpCookieToDict(cookie runtime.HTTPCookie) runtime.Dict {
	entries := map[string]runtime.DictEntry{}
	put := func(key string, value runtime.Value) {
		keyValue := runtime.String{Value: key}
		entries[DictKey(keyValue)] = runtime.DictEntry{Key: keyValue, Value: value}
	}
	put("name", runtime.String{Value: cookie.Name})
	put("value", runtime.String{Value: cookie.Value})
	put("path", runtime.String{Value: cookie.Path})
	put("domain", runtime.String{Value: cookie.Domain})
	if cookie.Expires == 0 {
		put("expires", runtime.Null{})
	} else {
		put("expires", runtime.NewInt64(cookie.Expires))
	}
	put("maxAge", runtime.NewInt64(cookie.MaxAge))
	put("secure", runtime.Bool{Value: cookie.Secure})
	put("httpOnly", runtime.Bool{Value: cookie.HTTPOnly})
	put("sameSite", runtime.String{Value: cookie.SameSite})
	return runtime.DictFromEntries(entries, nil)
}

func validateSameSite(value string) error {
	switch strings.ToLower(value) {
	case "", "default", "lax", "strict", "none":
		return nil
	default:
		return fmt.Errorf("sameSite must be default, lax, strict, or none")
	}
}

func nativeSameSite(value string) nethttp.SameSite {
	switch strings.ToLower(value) {
	case "lax":
		return nethttp.SameSiteLaxMode
	case "strict":
		return nethttp.SameSiteStrictMode
	case "none":
		return nethttp.SameSiteNoneMode
	default:
		return nethttp.SameSiteDefaultMode
	}
}

func sameSiteString(value nethttp.SameSite) string {
	switch value {
	case nethttp.SameSiteLaxMode:
		return "lax"
	case nethttp.SameSiteStrictMode:
		return "strict"
	case nethttp.SameSiteNoneMode:
		return "none"
	default:
		return "default"
	}
}
