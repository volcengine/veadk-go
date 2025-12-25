package ve_sign

import "testing"

func TestValidateOK(t *testing.T) {
	v := VeRequest{
		AK:      "ak",
		SK:      "sk",
		Method:  "POST",
		Scheme:  HttpsSchema,
		Host:    "open.volcengineapi.com",
		Path:    "/v1/test",
		Service: "open",
		Region:  "cn-beijing",
		Body:    []byte("{}"),
	}
	if err := v.validate(); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestValidateEmptyAK(t *testing.T) {
	v := VeRequest{SK: "sk", Method: "GET", Host: "h.com", Path: "/p", Service: "s", Region: "r"}
	if err := v.validate(); err == nil {
		t.Fatalf("expected error for empty AK")
	}
}

func TestValidateInvalidMethod(t *testing.T) {
	v := VeRequest{AK: "ak", SK: "sk", Method: "FOO", Host: "h.com", Path: "/p", Service: "s", Region: "r"}
	if err := v.validate(); err == nil {
		t.Fatalf("expected error for invalid method")
	}
}

func TestValidateInvalidHost(t *testing.T) {
	v := VeRequest{AK: "ak", SK: "sk", Method: "GET", Host: "", Path: "/p", Service: "s", Region: "r"}
	if err := v.validate(); err == nil {
		t.Fatalf("expected error for empty host")
	}
	v.Host = "h.com/extra"
	if err := v.validate(); err == nil {
		t.Fatalf("expected error for host containing path")
	}
}

func TestValidateInvalidPath(t *testing.T) {
	v := VeRequest{AK: "ak", SK: "sk", Method: "GET", Host: "h.com", Path: "p", Service: "s", Region: "r"}
	if err := v.validate(); err == nil {
		t.Fatalf("expected error for path not starting with /")
	}
}

func TestValidateEmptyBodyForPOST(t *testing.T) {
	v := VeRequest{AK: "ak", SK: "sk", Method: "POST", Host: "h.com", Path: "/p", Service: "s", Region: "r"}
	if err := v.validate(); err == nil {
		t.Fatalf("expected error for empty body on POST")
	}
}
