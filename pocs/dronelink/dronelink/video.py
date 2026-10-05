"""Camera ingest. ffmpeg decodes the source and emits MJPEG on stdout; we split frames
and fan the latest one out to browsers. Sources:
  test            synthetic test pattern (no drone needed)
  rtmp            listen on rtmp://0.0.0.0:1935/live/dji (point DJI Fly's custom RTMP here)
  <anything else> passed to ffmpeg -i as-is (file, rtsp://, ...)"""

import shutil
import subprocess
import threading

SOI, EOI = b"\xff\xd8", b"\xff\xd9"


def ffmpeg_cmd(source, rtmp_port=1935, fps=15):
    base = ["ffmpeg", "-hide_banner", "-loglevel", "error"]
    out = ["-an", "-r", str(fps), "-vf", "scale=960:-2", "-q:v", "6", "-f", "mjpeg", "-"]
    if source == "test":
        return base + ["-re", "-f", "lavfi", "-i", "testsrc2=size=960x540:rate=15"] + out
    if source == "rtmp":
        return base + ["-listen", "1", "-f", "flv", "-i", f"rtmp://0.0.0.0:{rtmp_port}/live/dji"] + out
    return base + ["-re", "-i", source] + out


def split_jpegs(buf):
    """Yield complete JPEG frames from buf; returns (frames, remainder)."""
    frames = []
    while True:
        s = buf.find(SOI)
        if s < 0:
            return frames, b""
        e = buf.find(EOI, s + 2)
        if e < 0:
            return frames, buf[s:]
        frames.append(buf[s:e + 2])
        buf = buf[e + 2:]


class Ingest:
    def __init__(self, source="test", rtmp_port=1935):
        self.source, self.rtmp_port = source, rtmp_port
        self.cond = threading.Condition()
        self.frame, self.seq, self.live, self._stop, self._proc = None, 0, False, False, None

    def start(self):
        if not shutil.which("ffmpeg"):
            raise RuntimeError("ffmpeg not found on PATH")
        threading.Thread(target=self._run, daemon=True).start()

    def _run(self):
        while not self._stop:
            self._proc = subprocess.Popen(ffmpeg_cmd(self.source, self.rtmp_port),
                                          stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
            rest = b""
            while not self._stop:
                chunk = self._proc.stdout.read1(65536)
                if not chunk:
                    break
                frames, rest = split_jpegs(rest + chunk)
                if frames:
                    with self.cond:
                        self.frame, self.seq, self.live = frames[-1], self.seq + 1, True
                        self.cond.notify_all()
            with self.cond:
                self.live = False
                self.cond.notify_all()
            if self.source != "rtmp":  # rtmp: re-listen for the next flight
                break

    def wait_frame(self, last_seq, timeout=5.0):
        with self.cond:
            self.cond.wait_for(lambda: self.seq != last_seq or self._stop, timeout)
            return self.seq, self.frame

    def close(self):
        self._stop = True
        if self._proc:
            self._proc.kill()
            self._proc.wait()
            self._proc.stdout.close()
        with self.cond:
            self.cond.notify_all()
