let stack = null;

function ensureStack() {
  if (!stack) {
    stack = document.createElement("div");
    stack.className = "toast-stack";
    document.body.appendChild(stack);
  }
  return stack;
}

export function showToast(message, { duration = 2400 } = {}) {
  const el = document.createElement("div");
  el.className = "toast";
  el.setAttribute("role", "status");
  el.textContent = message;
  ensureStack().appendChild(el);

  setTimeout(() => {
    el.classList.add("leaving");
    el.addEventListener("animationend", () => el.remove(), { once: true });
    // fallback removal in case animations are disabled (reduced motion)
    setTimeout(() => el.remove(), 400);
  }, duration);
}
