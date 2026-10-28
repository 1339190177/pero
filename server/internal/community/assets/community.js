// Native forms keep navigation and validation usable without JavaScript.
for (const form of document.querySelectorAll('form')) {
  form.addEventListener('submit', () => {
    const button = form.querySelector('button[type="submit"]');
    if (button) {
      button.dataset.submitting = 'true';
      button.disabled = true;
    }
    document.getElementById('busy').hidden = false;
  });
}
window.addEventListener('pageshow', () => {
  document.getElementById('busy').hidden = true;
  for (const button of document.querySelectorAll('[data-submitting]')) {
    button.disabled = false;
    delete button.dataset.submitting;
  }
});
