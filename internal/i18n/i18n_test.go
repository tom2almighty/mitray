package i18n

import (
	"reflect"
	"strings"
	"testing"
)

func formatVerbs(s string) []string {
	var verbs []string
	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			continue
		}
		if i+1 < len(s) && s[i+1] == '%' {
			i++
			continue
		}
		if i+1 < len(s) {
			verbs = append(verbs, s[i:i+2])
			i++
		}
	}
	return verbs
}

func TestAllMessagesPopulatedAndVerbsMatch(t *testing.T) {
	zhVal := reflect.ValueOf(zhMessages)
	enVal := reflect.ValueOf(enMessages)
	typ := zhVal.Type()

	for i := range typ.NumField() {
		name := typ.Field(i).Name
		zh := zhVal.Field(i).String()
		en := enVal.Field(i).String()
		if strings.TrimSpace(zh) == "" {
			t.Errorf("zhMessages.%s is empty", name)
		}
		if strings.TrimSpace(en) == "" {
			t.Errorf("enMessages.%s is empty", name)
		}
		zhVerbs := formatVerbs(zh)
		enVerbs := formatVerbs(en)
		if !reflect.DeepEqual(zhVerbs, enVerbs) {
			t.Errorf("field %s format verbs mismatch: zh=%v en=%v", name, zhVerbs, enVerbs)
		}
	}
}

func TestSetAndResolve(t *testing.T) {
	defer Set(ZH)

	Set(EN)
	if Code() != EN || T().WindowTitle != "MiTray Settings" {
		t.Fatalf("unexpected EN state: code=%q title=%q", Code(), T().WindowTitle)
	}
	Set(ZH)
	if Code() != ZH || T().WindowTitle != "MiTray 设置" {
		t.Fatalf("unexpected ZH state: code=%q title=%q", Code(), T().WindowTitle)
	}
	if Resolve(ZH) != ZH || Resolve(EN) != EN {
		t.Fatal("explicit language resolution failed")
	}
	if got := Resolve(Auto); got != ZH && got != EN {
		t.Fatalf("auto resolved to unexpected locale %q", got)
	}
}
