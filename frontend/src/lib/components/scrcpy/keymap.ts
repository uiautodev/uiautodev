import { AndroidKeyCode } from "@yume-chan/scrcpy";

export const androidKeyMap: Record<number, string> = {
  [AndroidKeyCode.AndroidHome]: "home",
  [AndroidKeyCode.AndroidBack]: "back",
  [AndroidKeyCode.AndroidAppSwitch]: "recents",
  [AndroidKeyCode.Power]: "power",
  [AndroidKeyCode.VolumeUp]: "volume_up",
  [AndroidKeyCode.VolumeDown]: "volume_down",
  [AndroidKeyCode.Tab]: "tab",
  [AndroidKeyCode.Space]: "space",
  [AndroidKeyCode.Enter]: "enter",
  [AndroidKeyCode.Backspace]: "backspace",
  [AndroidKeyCode.Delete]: "delete",
  [AndroidKeyCode.Escape]: "escape",
  [AndroidKeyCode.CapsLock]: "caps_lock",
  [AndroidKeyCode.PageUp]: "page_up",
  [AndroidKeyCode.PageDown]: "page_down",
  [AndroidKeyCode.Insert]: "insert",
  [AndroidKeyCode.ArrowUp]: "dpad_up",
  [AndroidKeyCode.ArrowDown]: "dpad_down",
  [AndroidKeyCode.ArrowLeft]: "dpad_left",
  [AndroidKeyCode.ArrowRight]: "dpad_right",
  [AndroidKeyCode.AndroidDPadCenter]: "dpad_center",
  [AndroidKeyCode.Comma]: "comma",
  [AndroidKeyCode.Period]: "period",
  [AndroidKeyCode.Minus]: "minus",
  [AndroidKeyCode.Equal]: "equals",
  [AndroidKeyCode.Semicolon]: "semicolon",
  [AndroidKeyCode.Quote]: "quote",
  [AndroidKeyCode.Slash]: "slash",
  [AndroidKeyCode.Backslash]: "backslash",
  [AndroidKeyCode.BracketLeft]: "left_bracket",
  [AndroidKeyCode.BracketRight]: "right_bracket",
  [AndroidKeyCode.Backquote]: "backquote",
  [AndroidKeyCode.Plus]: "plus",
  [AndroidKeyCode.At]: "at",
};

// a-z and 0-9 are consecutive in the Android keycode enum; map to their literal chars
for (let i = 0; i < 26; i++) {
  androidKeyMap[AndroidKeyCode.KeyA + i] = String.fromCharCode(97 + i);
}
for (let i = 0; i < 10; i++) {
  androidKeyMap[AndroidKeyCode.Digit0 + i] = String(i);
}

export function getKeyName(keyCode: AndroidKeyCode): string | undefined {
  return androidKeyMap[keyCode];
}
