// Creation form for the zibs.
// The token is sent only in the Authorization header of one fetch call and is
// cleared from the form on success. Nothing is written to cookies, storage,
// URLs, or anywhere else — the short link is displayed exactly once.
(() => {
  'use strict';

  const themeToggle = document.getElementById('theme-toggle');

  const renderThemeToggle = () => {
    const dark = document.documentElement.dataset.theme === 'dark';
    themeToggle.setAttribute('aria-label', dark ? 'Switch to light theme' : 'Switch to dark theme');
  };

  renderThemeToggle();
  themeToggle.addEventListener('click', () => {
    const next = document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark';
    document.documentElement.dataset.theme = next;
    try { localStorage.setItem('theme', next); } catch { /* storage unavailable */ }
    renderThemeToggle();
  });

  const form = document.getElementById('shorten-form');
  const destination = document.getElementById('destination');
  const token = document.getElementById('token');
  const submit = document.getElementById('submit');
  const errorBox = document.getElementById('error');
  const result = document.getElementById('result');
  const shortUrl = document.getElementById('short-url');
  const copyButton = document.getElementById('copy');
  const copyConfirm = document.getElementById('copy-confirm');
  const againButton = document.getElementById('again');
  let copyConfirmTimer;
  // The display drops the scheme for brevity (zibs.app/<code>), but the
  // clipboard always gets this full URL — a scheme-less string breaks in
  // href/Markdown contexts and strict auto-linkers.
  let shortLinkFull = '';

  const fadeDurationMs = (el) =>
    (parseFloat(getComputedStyle(el).transitionDuration) || 0) * 1000;

  const fadeOut = (el) => new Promise((resolve) => {
    el.classList.add('is-fading');
    setTimeout(() => {
      el.hidden = true;
      el.classList.remove('is-fading');
      resolve();
    }, fadeDurationMs(el));
  });

  const fadeIn = (el) => {
    el.hidden = false;
    el.classList.add('is-fading');
    requestAnimationFrame(() => {
      requestAnimationFrame(() => el.classList.remove('is-fading'));
    });
  };

  const aboutDialog = document.getElementById('about-dialog');
  const aboutOpen = document.getElementById('about-open');
  const aboutClose = document.getElementById('about-close');

  const openAbout = () => {
    aboutDialog.classList.add('is-fading');
    aboutDialog.showModal();
    requestAnimationFrame(() => {
      requestAnimationFrame(() => aboutDialog.classList.remove('is-fading'));
    });
  };

  // Dedicated flag rather than checking the is-fading class: the open
  // animation also uses that class, and its removal can be delayed in a
  // background tab — close must never be blocked by it.
  let aboutClosing = false;
  const closeAbout = () => {
    if (aboutClosing) return;
    aboutClosing = true;
    aboutDialog.classList.add('is-fading');
    setTimeout(() => {
      aboutDialog.close();
      aboutDialog.classList.remove('is-fading');
      aboutClosing = false;
    }, fadeDurationMs(aboutDialog));
  };

  aboutOpen.addEventListener('click', openAbout);
  aboutClose.addEventListener('click', closeAbout);
  // Esc: fade out instead of the instant native close.
  aboutDialog.addEventListener('cancel', (event) => {
    event.preventDefault();
    closeAbout();
  });
  // Backdrop click: the padded .about-card covers the whole dialog box, so a
  // click landing on the dialog element itself can only be on the backdrop.
  aboutDialog.addEventListener('click', (event) => {
    if (event.target === aboutDialog) closeAbout();
  });

  const showError = (message) => {
    result.hidden = true;
    shortUrl.textContent = '';
    shortLinkFull = '';
    errorBox.textContent = message;
    errorBox.hidden = false;
  };

  form.addEventListener('submit', async (event) => {
    event.preventDefault();
    errorBox.hidden = true;
    result.hidden = true;
    shortUrl.textContent = '';
    shortLinkFull = '';

    const url = destination.value.trim();
    const tokenValue = token.value.trim();
    if (url === '') {
      showError('Enter a link to shorten.');
      return;
    }
    if (tokenValue === '') {
      showError('Enter your zib token.');
      return;
    }

    submit.disabled = true;
    // Show the loading label only if the request is still pending after a
    // beat, so fast responses go straight to the fade with no text flicker.
    const loadingTimer = setTimeout(() => { submit.textContent = 'Zibbing…'; }, 300);
    try {
      const response = await fetch('/links', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': 'Bearer ' + tokenValue,
        },
        body: JSON.stringify({ url }),
      });
      clearTimeout(loadingTimer);

      if (response.status === 201) {
        const body = await response.json();
        const link = new URL('/' + body.code, window.location.origin);
        shortLinkFull = link.toString();
        shortUrl.textContent = link.host + link.pathname;
        clearTimeout(copyConfirmTimer);
        copyConfirm.hidden = true;
        copyButton.hidden = false;
        form.reset();
        await fadeOut(form);
        fadeIn(result);
        copyButton.focus({ preventScroll: true });
      } else if (response.status === 401) {
        showError('The token was rejected — it may have been revoked or run out of uses. Ask for a fresh one.');
      } else if (response.status === 400) {
        showError(await response.text());
      } else {
        showError('Something went wrong on the server. Try again in a moment.');
      }
    } catch {
      showError('Network error — the request never made it. Check the connection and try again.');
    } finally {
      clearTimeout(loadingTimer);
      // On success this runs after the form has faded out, so the reset is
      // never visible mid-fade; on errors it restores the button right away.
      submit.disabled = false;
      submit.textContent = 'Zib it';
    }
  });

  againButton.addEventListener('click', async () => {
    await fadeOut(result);
    shortUrl.textContent = '';
    shortLinkFull = '';
    fadeIn(form);
    destination.focus({ preventScroll: true });
  });

  copyButton.addEventListener('click', async () => {
    try {
      await navigator.clipboard.writeText(shortLinkFull);
    } catch {
      const selection = window.getSelection();
      const range = document.createRange();
      range.selectNodeContents(shortUrl);
      selection.removeAllRanges();
      selection.addRange(range);
      return;
    }
    await fadeOut(copyButton);
    fadeIn(copyConfirm);
    clearTimeout(copyConfirmTimer);
    copyConfirmTimer = setTimeout(async () => {
      await fadeOut(copyConfirm);
      fadeIn(copyButton);
      // Restore focus for keyboard users if hiding the button dropped it.
      if (document.activeElement === document.body) {
        copyButton.focus({ preventScroll: true });
      }
    }, 2500);
  });
})();
