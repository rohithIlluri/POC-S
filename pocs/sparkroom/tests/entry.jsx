// Bundle entry for the jsdom harness: mounts the POC component into #root.
// The harness installs its window.storage / fetch mocks *before* requiring the
// bundle, so the component sees them on first render.
import { createRoot } from "react-dom/client";
import SparkroomPOC from "../app/sparkroom.jsx";

createRoot(document.getElementById("root")).render(<SparkroomPOC />);
