package device

import (
	"fmt"
	"strings"
)

// KeyName is a logical key that can be pressed on a device.
type KeyName string

const (
	KeyHome       KeyName = "home"
	KeyBack       KeyName = "back"
	KeyMenu       KeyName = "menu"
	KeyRecents    KeyName = "recents"
	KeyPower      KeyName = "power"
	KeyVolumeUp   KeyName = "volume_up"
	KeyVolumeDown KeyName = "volume_down"

	KeyTab       KeyName = "tab"
	KeySpace     KeyName = "space"
	KeyEnter     KeyName = "enter"
	KeyEscape    KeyName = "escape"
	KeyBackspace KeyName = "backspace"
	KeyDelete    KeyName = "delete"
	KeyCapsLock  KeyName = "caps_lock"
	KeyPageUp    KeyName = "page_up"
	KeyPageDown  KeyName = "page_down"
	KeyInsert    KeyName = "insert"

	KeyDpadUp     KeyName = "dpad_up"
	KeyDpadDown   KeyName = "dpad_down"
	KeyDpadLeft   KeyName = "dpad_left"
	KeyDpadRight  KeyName = "dpad_right"
	KeyDpadCenter KeyName = "dpad_center"

	KeyComma        KeyName = "comma"
	KeyPeriod       KeyName = "period"
	KeyMinus        KeyName = "minus"
	KeyEquals       KeyName = "equals"
	KeySemicolon    KeyName = "semicolon"
	KeyQuote        KeyName = "quote"
	KeySlash        KeyName = "slash"
	KeyBackslash    KeyName = "backslash"
	KeyLeftBracket  KeyName = "left_bracket"
	KeyRightBracket KeyName = "right_bracket"
	KeyBackquote    KeyName = "backquote"
	KeyPlus         KeyName = "plus"
	KeyAt           KeyName = "at"
)

// ParseKeyName converts a case-insensitive key name to a KeyName.
// Single characters a-z and 0-9 are valid key names.
func ParseKeyName(s string) (KeyName, error) {
	key := KeyName(strings.ToLower(s))
	if !key.Valid() {
		return "", fmt.Errorf("unsupported key name: %s", s)
	}
	return key, nil
}

// MetaState is a bitmask of modifier keys held during a key event.
// It mirrors Android's AndroidKeyEventMeta.
type MetaState int

const (
	MetaNone       MetaState = 0
	MetaShift      MetaState = 0x01
	MetaAlt        MetaState = 0x02
	MetaAltLeft    MetaState = 0x10
	MetaAltRight   MetaState = 0x20
	MetaShiftLeft  MetaState = 0x40
	MetaShiftRight MetaState = 0x80
	MetaCtrl       MetaState = 0x1000
	MetaCtrlLeft   MetaState = 0x2000
	MetaCtrlRight  MetaState = 0x4000
	MetaMeta       MetaState = 0x10000
	MetaMetaLeft   MetaState = 0x20000
	MetaMetaRight  MetaState = 0x40000
)

// Valid reports whether k is a known logical key.
func (k KeyName) Valid() bool {
	switch k {
	case KeyHome, KeyBack, KeyMenu, KeyRecents, KeyPower, KeyVolumeUp, KeyVolumeDown,
		KeyTab, KeySpace, KeyEnter, KeyEscape, KeyBackspace, KeyDelete, KeyCapsLock,
		KeyPageUp, KeyPageDown, KeyInsert, KeyDpadUp, KeyDpadDown, KeyDpadLeft, KeyDpadRight, KeyDpadCenter,
		KeyComma, KeyPeriod, KeyMinus, KeyEquals, KeySemicolon, KeyQuote, KeySlash,
		KeyBackslash, KeyLeftBracket, KeyRightBracket, KeyBackquote, KeyPlus, KeyAt:
		return true
	}
	// Single character keys: a-z and 0-9
	if len(k) == 1 {
		c := k[0]
		return (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')
	}
	return false
}
