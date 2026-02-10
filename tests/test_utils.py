import unittest

from utils import extract_text


class UtilsTests(unittest.TestCase):
    def test_extract_text_handles_none(self):
        self.assertEqual(extract_text(None), "")


if __name__ == "__main__":
    unittest.main()
