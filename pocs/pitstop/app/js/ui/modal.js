let dialogEl = null;

function ensureDialog() {
  if (!dialogEl) dialogEl = document.getElementById("modal");
  return dialogEl;
}

export function openModal({ title, bodyHtml, buttons }) {
  const dialog = ensureDialog();
  dialog.innerHTML = `
    <form method="dialog">
      <h3>${title}</h3>
      <div class="modal-body">${bodyHtml}</div>
      <div class="form-actions">${buttons}</div>
    </form>
  `;
  dialog.showModal();
  return dialog;
}

export function closeModal() {
  const dialog = ensureDialog();
  if (dialog.open) dialog.close();
}
