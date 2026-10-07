"""Regression tests for startup UI inspection on flaky 16 KB emulators."""
import unittest
import xml.etree.ElementTree as ET

from startup_ui import (
    center_of,
    find_tab,
    foreground_anr_title,
    pixel_launcher_anr_close_button,
)


def dialog(title, with_close=True):
    root = ET.Element("hierarchy")
    ET.SubElement(root, "node", {
        "resource-id": "android:id/alertTitle",
        "text": title,
    })
    if with_close:
        ET.SubElement(root, "node", {
            "resource-id": "android:id/aerr_close",
            "text": "Close app",
            "bounds": "[70,898][1010,1024]",
        })
    return root


class StartupUITests(unittest.TestCase):
    def test_tab_visible_on_healthy_activity(self):
        root = ET.fromstring(
            '<hierarchy><node text="REMUX/DEMUX" bounds="[0,1][100,51]"/>'
            '<node text="ADVANCED" bounds="[100,1][200,51]"/></hierarchy>'
        )
        self.assertEqual(find_tab(root, "remux/demux").get("text"), "REMUX/DEMUX")
        self.assertEqual(center_of(find_tab(root, "ADVANCED")), (150, 26))
        self.assertIsNone(pixel_launcher_anr_close_button(root))

    def test_dismiss_specific_external_launcher_anr(self):
        # Matches the system alert overlay from the real failing 16 KB run.
        root = dialog("Pixel Launcher isn't responding")
        self.assertIsNone(find_tab(root, "REMUX/DEMUX"))
        self.assertEqual(foreground_anr_title(root), "Pixel Launcher isn't responding")
        self.assertEqual(center_of(pixel_launcher_anr_close_button(root)), (540, 961))

    def test_never_dismiss_mattrip_anr(self):
        root = dialog("MattRip isn't responding")
        self.assertIsNone(pixel_launcher_anr_close_button(root))

    def test_never_dismiss_other_system_dialog(self):
        root = dialog("Android System isn't responding")
        self.assertIsNone(pixel_launcher_anr_close_button(root))

    def test_missing_close_button_is_not_ignored(self):
        root = dialog("Pixel Launcher isn't responding", with_close=False)
        self.assertIsNone(pixel_launcher_anr_close_button(root))

    def test_invalid_bounds_raise(self):
        element = ET.Element("node", {"bounds": "broken"})
        with self.assertRaises(ValueError):
            center_of(element)


if __name__ == "__main__":
    unittest.main()
