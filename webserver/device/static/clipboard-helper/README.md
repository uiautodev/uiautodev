# clipboard-helper

yadb 的 `-readClipboard` / `-writeClipboard` 只处理文本，系统剪贴板里的图片是
`ClipData` 里的一个 `content://` URI，必须通过 `IClipboard` 直接读写。这个
`app_process` 小工具补上这两条命令：

```bash
# 打印当前剪贴板：empty / uri:<uri> + mime:<mime> / text:<text>
adb shell app_process -Djava.class.path=/data/local/tmp/uiautodev-clipboard-1.jar \
  /data/local/tmp com.uiautodev.clipboard.Main -getClip

# 把剪贴板指向一个已有的 content URI（图片由 MediaStore 提供）
adb shell app_process -Djava.class.path=/data/local/tmp/uiautodev-clipboard-1.jar \
  /data/local/tmp com.uiautodev.clipboard.Main -setClipUri content://media/external/images/media/42 image/png
```

`../clipboard-helper.jar` 是编译产物，由 Go 端嵌入并在设备上缺失或大小不一致时
推送。修改 `Main.java` 后执行 `./build.sh` 重新生成（需要 Android SDK），并同时
提升 `webserver/device/android_clipboard.go` 里的 `clipboardHelperVersion`，让设备
上的旧副本被替换。
