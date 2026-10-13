"""图片编辑部署配置的只读预检回归。"""
from pathlib import Path
import tempfile
import unittest

from creative_image_edit_config import check


class ImageEditConfigTests(unittest.TestCase):
    def test_config_matrix(self):
        for api, worker, valid in [
            ("", None, True),
            ("CREATIVE_IMAGE_EDIT=off\nCREATIVE_MEDIA_GC_MODE=typo", None, True),
            ("CREATIVE_IMAGE_EDIT=on", None, False),
            ("CREATIVE_IMAGE_EDIT=on", "CREATIVE_IMAGE_EDIT=off", False),
            ("CREATIVE_IMAGE_EDIT=off", "CREATIVE_IMAGE_EDIT=on", False),
            ("CREATIVE_IMAGE_EDIT=on\nCREATIVE_MEDIA_GC_MODE=delete", "CREATIVE_IMAGE_EDIT=on\nCREATIVE_MEDIA_GC_MODE=scan", False),
            ("CREATIVE_IMAGE_EDIT=on\nCREATIVE_MEDIA_GC_MODE=scan", "CREATIVE_IMAGE_EDIT=on\nCREATIVE_MEDIA_GC_MODE=delete", False),
            ("CREATIVE_IMAGE_EDIT=' on '\nCREATIVE_MEDIA_GC_MODE=delete", 'CREATIVE_IMAGE_EDIT="on"\nCREATIVE_MEDIA_GC_MODE=delete', True),
            ("CREATIVE_IMAGE_EDIT=off\nCREATIVE_IMAGE_EDIT=on", None, False),
            ("CREATIVE_IMAGE_EDIT=$(false)", None, False),
        ]:
            with self.subTest(api=api, worker=worker), tempfile.TemporaryDirectory() as root:
                a, w = Path(root) / "api.env", Path(root) / "worker.env"
                a.write_text(api)
                if worker is not None:
                    w.write_text(worker)
                if valid:
                    check(a, w if worker is not None else None)
                else:
                    with self.assertRaises(ValueError):
                        check(a, w if worker is not None else None)


if __name__ == "__main__":
    unittest.main()
