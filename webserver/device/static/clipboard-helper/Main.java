package com.uiautodev.clipboard;

import android.content.ClipData;
import android.content.ClipDescription;
import android.net.Uri;
import android.os.Build;

import java.lang.reflect.Method;

/**
 * Minimal clipboard bridge for content URIs (images). Text stays in yadb.
 *
 * Usage:
 *   app_process -Djava.class.path=<jar> /data/local/tmp com.uiautodev.clipboard.Main -getClip
 *   app_process -Djava.class.path=<jar> /data/local/tmp com.uiautodev.clipboard.Main -setClipUri <uri> <mime>
 */
public final class Main {
    private static final String PACKAGE_NAME = "com.android.shell";
    private static final int USER_ID = 0;

    public static void main(String[] args) {
        try {
            if (args.length == 3 && "-setClipUri".equals(args[0])) {
                setClipUri(args[1], args[2]);
                System.out.println("setClipUri success");
            } else if (args.length == 1 && "-getClip".equals(args[0])) {
                System.out.println(getClip());
            } else {
                System.out.println("Invalid argument");
            }
        } catch (Throwable t) {
            System.out.println("MainException: " + t);
        }
        System.exit(0);
    }

    private static Method method(Class<?> cls, String name, Class<?>... parameterTypes) throws Exception {
        return cls.getMethod(name, parameterTypes);
    }

    private static Object service() throws Exception {
        Class<?> serviceManager = Class.forName("android.os.ServiceManager");
        Object binder = serviceManager.getMethod("getService", String.class).invoke(null, "clipboard");
        Class<?> stub = Class.forName("android.content.IClipboard$Stub");
        return stub.getMethod("asInterface", Class.forName("android.os.IBinder")).invoke(null, binder);
    }

    private static ClipData getPrimaryClip(Object service) throws Exception {
        Class<?> iclipboard = Class.forName("android.content.IClipboard");
        int sdk = Build.VERSION.SDK_INT;
        if (sdk < 29) {
            return (ClipData) method(iclipboard, "getPrimaryClip", String.class).invoke(service, PACKAGE_NAME);
        } else if (sdk < 33) {
            return (ClipData) method(iclipboard, "getPrimaryClip", String.class, int.class).invoke(service, PACKAGE_NAME, USER_ID);
        } else if (sdk < 34) {
            try {
                return (ClipData) method(iclipboard, "getPrimaryClip", String.class, String.class, int.class).invoke(service, PACKAGE_NAME, null, USER_ID);
            } catch (NoSuchMethodException e) {
                return (ClipData) method(iclipboard, "getPrimaryClip", String.class, int.class).invoke(service, PACKAGE_NAME, USER_ID);
            }
        }
        try {
            return (ClipData) method(iclipboard, "getPrimaryClip", String.class, String.class, int.class, int.class).invoke(service, PACKAGE_NAME, null, USER_ID, 0);
        } catch (NoSuchMethodException e) {
            try {
                return (ClipData) method(iclipboard, "getPrimaryClip", String.class, String.class, int.class, int.class, String.class).invoke(service, PACKAGE_NAME, null, USER_ID, 0, null);
            } catch (NoSuchMethodException error) {
                return (ClipData) method(iclipboard, "getPrimaryClip", String.class, String.class, String.class, String.class, int.class, int.class, boolean.class)
                        .invoke(service, PACKAGE_NAME, null, null, null, USER_ID, 0, false);
            }
        }
    }

    private static void setPrimaryClip(Object service, ClipData clip) throws Exception {
        Class<?> iclipboard = Class.forName("android.content.IClipboard");
        int sdk = Build.VERSION.SDK_INT;
        if (sdk < 29) {
            method(iclipboard, "setPrimaryClip", ClipData.class, String.class).invoke(service, clip, PACKAGE_NAME);
        } else if (sdk < 33) {
            method(iclipboard, "setPrimaryClip", ClipData.class, String.class, int.class).invoke(service, clip, PACKAGE_NAME, USER_ID);
        } else if (sdk < 34) {
            try {
                method(iclipboard, "setPrimaryClip", ClipData.class, String.class, String.class, int.class).invoke(service, clip, PACKAGE_NAME, null, USER_ID);
            } catch (NoSuchMethodException e) {
                method(iclipboard, "setPrimaryClip", ClipData.class, String.class, int.class).invoke(service, clip, PACKAGE_NAME, USER_ID);
            }
        } else {
            try {
                method(iclipboard, "setPrimaryClip", ClipData.class, String.class, String.class, int.class, int.class).invoke(service, clip, PACKAGE_NAME, null, USER_ID, 0);
            } catch (NoSuchMethodException e) {
                method(iclipboard, "setPrimaryClip", ClipData.class, String.class, String.class, int.class, int.class, boolean.class).invoke(service, clip, PACKAGE_NAME, null, USER_ID, 0, false);
            }
        }
    }

    private static String getClip() throws Exception {
        ClipData clip = getPrimaryClip(service());
        if (clip == null || clip.getItemCount() == 0) {
            return "empty";
        }
        ClipData.Item item = clip.getItemAt(0);
        ClipDescription description = clip.getDescription();
        Uri uri = item.getUri();
        if (uri != null) {
            String mime = null;
            int count = description == null ? 0 : description.getMimeTypeCount();
            for (int i = 0; i < count; i++) {
                String candidate = description.getMimeType(i);
                if (candidate != null && candidate.startsWith("image/")) {
                    mime = candidate;
                    break;
                }
            }
            if (mime == null && count > 0) {
                mime = description.getMimeType(0);
            }
            return "uri:" + uri + "\nmime:" + (mime == null ? "image/png" : mime);
        }
        CharSequence text = item.getText();
        if (text != null) {
            return "text:" + text;
        }
        return "other";
    }

    private static void setClipUri(String uriString, String mime) throws Exception {
        Uri uri = Uri.parse(uriString);
        ClipData clip = new ClipData("image", new String[]{mime}, new ClipData.Item(uri));
        setPrimaryClip(service(), clip);
    }
}
