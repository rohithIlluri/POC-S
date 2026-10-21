// Progressive enhancement only: every feature works without this file.
(() => {
  // Show the "name" field for event metrics (symptom, med).
  const metric = document.getElementById("metric");
  const labelField = document.getElementById("label-field");
  if (metric && labelField) {
    const sync = () => {
      labelField.hidden = metric.selectedOptions[0]?.dataset.event !== "true";
    };
    metric.addEventListener("change", sync);
    sync();
  }

  // Chart hover: crosshair + tooltip on the nearest day, with a hit area
  // that is the whole plot rather than the tiny markers.
  for (const fig of document.querySelectorAll("[data-chart]")) {
    const svg = fig.querySelector("svg");
    const tip = fig.querySelector(".tip");
    const cross = fig.querySelector(".cross");
    const marker = fig.querySelector(".marker");
    const pts = [...fig.querySelectorAll(".pt")];
    if (!svg || !tip || !marker || !pts.length) continue;
    const vbWidth = svg.viewBox.baseVal.width;
    const vbHeight = svg.viewBox.baseVal.height;
    // The SVG stretches to its box, so markers are HTML dots placed in
    // percentages — an SVG circle would be squashed into an ellipse.
    const place = (p) => {
      marker.style.left = `${(p.cx.baseVal.value / vbWidth) * 100}%`;
      marker.style.top = `${(p.cy.baseVal.value / vbHeight) * 100}%`;
      marker.hidden = false;
    };
    const rest = () => {
      if (marker.hasAttribute("data-single")) place(pts[0]);
      else marker.hidden = true;
    };
    rest();

    const show = (clientX) => {
      const box = svg.getBoundingClientRect();
      const x = ((clientX - box.left) / box.width) * vbWidth;
      let best = pts[0];
      for (const p of pts) {
        if (Math.abs(p.cx.baseVal.value - x) < Math.abs(best.cx.baseVal.value - x)) best = p;
      }
      place(best);
      const px = best.cx.baseVal.value;
      cross.setAttribute("x1", px);
      cross.setAttribute("x2", px);
      cross.hidden = false;
      tip.textContent = best.dataset.tip;
      tip.hidden = false;
      tip.style.left = `${(px / vbWidth) * 100}%`;
    };
    const hide = () => {
      tip.hidden = true;
      cross.hidden = true;
      rest();
    };
    svg.addEventListener("pointermove", (e) => show(e.clientX));
    svg.addEventListener("pointerdown", (e) => show(e.clientX));
    svg.addEventListener("pointerleave", hide);
  }
})();
