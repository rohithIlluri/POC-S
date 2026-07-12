// SVG gauge-ring builders. Rings are decorative reinforcement: status is always
// also conveyed by the adjacent text label, never by color alone.

const STATUS_COLOR = {
  ok: "var(--ok)",
  due_soon: "var(--due-soon)",
  overdue: "var(--overdue)",
  unknown: "var(--unknown)",
};

// progress: 0..1 fraction of the ring to fill (already clamped by caller).
// Returns SVG markup; the stroke-dashoffset transition in CSS animates the
// sweep when `animateFrom0` set the starting offset first (see animateRings).
export function ringSvg({ progress, status, size, strokeWidth }) {
  const r = (size - strokeWidth) / 2;
  const c = 2 * Math.PI * r;
  const offset = c * (1 - progress);
  return `
    <svg viewBox="0 0 ${size} ${size}" aria-hidden="true">
      <circle class="ring-track" cx="${size / 2}" cy="${size / 2}" r="${r}"
        fill="none" stroke-width="${strokeWidth}" />
      <circle class="ring-value ${status}" cx="${size / 2}" cy="${size / 2}" r="${r}"
        fill="none" stroke="${STATUS_COLOR[status]}" stroke-width="${strokeWidth}"
        stroke-linecap="round" stroke-dasharray="${c}" stroke-dashoffset="${c}"
        data-target-offset="${offset}" />
    </svg>
  `;
}

// Kicks off the sweep animation for every ring inside `root`: each ring was
// rendered fully empty (offset = circumference), and on the next frame we set
// its real offset so the CSS transition draws it in.
export function animateRings(root) {
  requestAnimationFrame(() => {
    requestAnimationFrame(() => {
      for (const circle of root.querySelectorAll(".ring-value[data-target-offset]")) {
        circle.style.strokeDashoffset = circle.dataset.targetOffset;
      }
    });
  });
}
