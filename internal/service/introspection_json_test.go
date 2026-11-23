package service

import (
	"reflect"
	"testing"
)

// Cookie Clicker writes this line at import; the object stored in production started with it.
const cookieClickerPrefix = "ℹ️🍪 This Cookie Clicker apworld does not include a manifest file, and will not until this issue is resolved > https://github.com/ArchipelagoMW/Archipelago/issues/5585"

func TestIntrospectionJSON_TextBeforeTheJSONIsDropped(t *testing.T) {
	raw := []byte(cookieClickerPrefix + "\nCopied vendor/linux_313.zip to /tmp/x/linux_313.zip\n{\"options\":{},\"locations\":[\"Cursor 1\"]}\n")

	object, prefix := introspectionJSON(raw)
	got, err := parseApworldLocations(object)

	if err != nil || !reflect.DeepEqual(got, []string{"Cursor 1"}) {
		t.Fatalf("got %v, %v", got, err)
	}
	if want := cookieClickerPrefix + "\nCopied vendor/linux_313.zip to /tmp/x/linux_313.zip"; string(prefix) != want {
		t.Fatalf("prefix = %q", prefix)
	}
}

func TestIntrospectionJSON_CleanJSONIsUntouched(t *testing.T) {
	raw := []byte(`{"options":{},"locations":["A"]}`)

	object, prefix := introspectionJSON(raw)

	if string(object) != string(raw) || prefix != nil {
		t.Fatalf("object = %q, prefix = %q", object, prefix)
	}
}

// Without a line opening with "{" there is nothing to recover: the read still fails as unreadable.
func TestIntrospectionJSON_NoJSONLineStaysUnreadable(t *testing.T) {
	raw := []byte("Traceback (most recent call last):\n  oops")

	object, prefix := introspectionJSON(raw)

	if string(object) != string(raw) || prefix != nil {
		t.Fatalf("object = %q, prefix = %q", object, prefix)
	}
	if _, err := parseApworldLocations(object); err == nil {
		t.Fatal("want an unreadable sidecar")
	}
}
