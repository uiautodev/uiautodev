import glob
import os.path

application = defines.get("app", "/System/Applications/TextEdit.app")  # noqa: F821
appname = os.path.basename(application)

# 生成方式见 build/dmg/make-background.sh（background@2x.png 会被 dmgbuild 自动合成为 HiDPI tiff）
background = defines.get("background", "build/dmg/background.png")  # noqa: F821

format = defines.get("format", "UDZO")  # noqa: F821
size = defines.get("size", None)  # noqa: F821

files = [application]
symlinks = {"Applications": "/Applications"}
badge_icon = glob.glob(os.path.join(application, "Contents", "Resources", "*.icns"))[0]

icon_locations = {appname: (183, 170), "Applications": (537, 170)}

show_status_bar = False
show_tab_view = False
show_toolbar = False
show_pathbar = False
show_sidebar = False
sidebar_width = 180

window_rect = ((200, 200), (720, 468))
default_view = "icon-view"
show_icon_preview = False
include_icon_view_settings = "auto"
include_list_view_settings = "auto"

arrange_by = None
grid_offset = (0, 0)
grid_spacing = 100
scroll_position = (0, 0)
label_pos = "bottom"
text_size = 14
icon_size = 128
