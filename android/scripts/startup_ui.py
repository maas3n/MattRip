"""Small, deterministic helpers for Android startup UI inspection.

Keep these pure so emulator/system dialogs can be regression-tested without an emulator.
"""


def find_tab(root, label):
    """Return the exact visible tab label from a uiautomator XML tree."""
    return next(
        (node for node in root.iter("node") if node.get("text", "").casefold() == label.casefold()),
        None,
    )


def foreground_anr_title(root):
    """Return the foreground Android ANR dialog title, if any."""
    return next(
        (node.get("text", "") for node in root.iter("node")
         if node.get("resource-id") == "android:id/alertTitle"),
        None,
    )


def pixel_launcher_anr_close_button(root):
    """Only dismiss the known Pixel Launcher *system* ANR, never an app ANR.

    The 16 KB-page emulator can briefly show this dialog *above* the correctly
    launched MattRip activity after its system_server crashes during cold boot.
    """
    if foreground_anr_title(root) != "Pixel Launcher isn't responding":
        return None
    return next(
        (node for node in root.iter("node")
         if node.get("resource-id") == "android:id/aerr_close"
         and node.get("text") == "Close app"),
        None,
    )


def center_of(node):
    """Input tap center for a standard Android UIAutomator bounds attribute."""
    import re
    coords = [int(value) for value in re.findall(r"\d+", node.get("bounds", ""))]
    if len(coords) != 4:
        raise ValueError("Invalid UI element bounds")
    x1, y1, x2, y2 = coords
    return (x1 + x2) // 2, (y1 + y2) // 2
