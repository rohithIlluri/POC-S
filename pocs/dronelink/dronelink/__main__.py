import argparse
import os

from . import __version__
from .drones import BridgeDrone, SimDrone, ViewerOnly
from .server import make_server
from .video import Ingest


def main(argv=None):
    p = argparse.ArgumentParser(prog="dronelink", description=__doc__)
    p.add_argument("--version", action="version", version=__version__)
    p.add_argument("--host", default="127.0.0.1", help="default localhost; there is no auth, don't expose it")
    p.add_argument("--port", type=int, default=8080)
    p.add_argument("--video", default="test", help="test | rtmp | <ffmpeg input> | none")
    p.add_argument("--rtmp-port", type=int, default=1935)
    p.add_argument("--viewer", action="store_true", help="camera only, no drone control (iPhone + DJI Fly)")
    p.add_argument("--bridge", help="URL of the phone-side DJI bridge; omit for the built-in simulated drone")
    p.add_argument("--bridge-token", default=os.environ.get("DRONELINK_BRIDGE_TOKEN"), help="PIN shown on the phone (or env DRONELINK_BRIDGE_TOKEN)")
    p.add_argument("--allow-flight", action="store_true", help="let the UI send takeoff/land to a real bridge")
    a = p.parse_args(argv)
    drone = ViewerOnly() if a.viewer else BridgeDrone(a.bridge, a.allow_flight, token=a.bridge_token) if a.bridge else SimDrone()
    ingest = None if a.video == "none" else Ingest(a.video, a.rtmp_port)
    if ingest:
        ingest.start()
    srv = make_server(drone, ingest, a.host, a.port)
    print(f"dronelink {__version__}: http://{a.host}:{a.port}  drone={drone.kind} video={a.video}")
    if a.video == "rtmp":
        print(f"point DJI Fly custom RTMP at rtmp://<this-machine>:{a.rtmp_port}/live/dji")
    try:
        srv.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        drone.close()
        if ingest:
            ingest.close()


if __name__ == "__main__":
    main()
