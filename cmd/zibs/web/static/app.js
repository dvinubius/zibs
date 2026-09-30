// Creation form for the zibs.
// The token is sent only in the Authorization header of one fetch call. After
// a request the token accepts, it is kept in this browser's local storage so
// the visitor doesn't retype it on this device; nothing else — no cookies, no
// URL parameters, no history of the links — is written anywhere, and the short
// link is displayed exactly once.
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
  const intro = document.getElementById('intro');
  const tokenHint = document.getElementById('token-hint');
  const savedTokenNote = document.getElementById('saved-token-note');
  const savedTokenUsesLeft = document.getElementById('saved-token-uses-left');
  const usesLeftNote = document.getElementById('uses-left-note');
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

  // Token memory. A device that has already zibbed once keeps its token: the
  // token field is then filled in from storage and locked, and the intro that
  // explains how to get a token steps aside.
  // Storage can throw (private modes, blocked site data); every access is
  // guarded and the page falls back to asking for the token each time.
  const tokenStorageKey = 'zib-token';
  // The uses a request last reported are kept with the token, so the form can
  // mention them before the next request rather than only after it.
  const usesLeftStorageKey = 'zib-token-uses-left';
  // Only the last few uses are worth mentioning; a healthy token stays quiet.
  const lowUsesThreshold = 3;

  // The count is the one accented word in either note, so both build it the
  // same way and surround it with their own muted copy.
  const usesLeftCount = (usesLeft) => {
    const count = document.createElement('span');
    count.className = 'uses-left-count';
    count.textContent = `${usesLeft} use${usesLeft === 1 ? '' : 's'} left`;
    return count;
  };

  const readSavedToken = () => {
    try { return localStorage.getItem(tokenStorageKey) || ''; } catch { return ''; }
  };
  // null whenever the count is unknown — nothing stored, or a value another
  // tab or an older version of this page left behind.
  const readSavedUsesLeft = () => {
    try {
      const stored = localStorage.getItem(usesLeftStorageKey);
      const usesLeft = Number(stored);
      return stored !== null && Number.isInteger(usesLeft) && usesLeft >= 0 ? usesLeft : null;
    } catch { return null; }
  };
  let savedToken = readSavedToken();
  let savedUsesLeft = readSavedUsesLeft();

  const rememberToken = (value, usesLeft) => {
    savedToken = value;
    savedUsesLeft = typeof usesLeft === 'number' ? usesLeft : null;
    const usesLeftToStore = savedUsesLeft;
    try {
      localStorage.setItem(tokenStorageKey, value);
      if (usesLeftToStore === null) {
        localStorage.removeItem(usesLeftStorageKey);
      } else {
        localStorage.setItem(usesLeftStorageKey, String(usesLeftToStore));
      }
    } catch { /* storage unavailable */ }
    renderTokenSource();
  };

  const forgetToken = () => {
    savedToken = '';
    savedUsesLeft = null;
    try {
      localStorage.removeItem(tokenStorageKey);
      localStorage.removeItem(usesLeftStorageKey);
    } catch { /* storage unavailable */ }
    renderTokenSource();
  };

  // The result replaces the form, and the intro explains how to get a token
  // for the form — so it steps aside while a short link is on screen, saved
  // token or not.
  let showingResult = false;
  const setShowingResult = (value) => {
    showingResult = value;
    renderTokenSource();
  };

  // Collapsing the intro the instant a link is created pulls the page up
  // while the form is still fading out, which reads as a glitch. Hiding
  // therefore waits out that fade; showing is immediate, so the form never
  // fades in over a page that is still closing up.
  let introHideTimer;
  let introHidingWaitsForFade = false;
  const renderIntro = (shouldHide) => {
    clearTimeout(introHideTimer);
    if (!shouldHide) {
      intro.hidden = false;
      return;
    }
    if (intro.hidden || !introHidingWaitsForFade) {
      intro.hidden = true;
      return;
    }
    introHideTimer = setTimeout(() => { intro.hidden = true; }, fadeDurationMs(form));
  };

  function renderTokenSource() {
    const hasSavedToken = savedToken !== '';
    renderIntro(hasSavedToken || showingResult);
    // Filled in and disabled rather than removed: the field stays where the
    // visitor expects it and shows, without a word, that it is taken care of.
    if (hasSavedToken) {
      token.value = savedToken;
    } else if (token.disabled) {
      // The field was holding a token that has just been dropped; the
      // visitor is being asked for a new one, so don't leave the old one in.
      token.value = '';
    }
    token.disabled = hasSavedToken;
    tokenHint.hidden = hasSavedToken;
    savedTokenNote.hidden = !hasSavedToken;
    const warnAboutUses = savedUsesLeft !== null && savedUsesLeft <= lowUsesThreshold;
    if (warnAboutUses) {
      savedTokenUsesLeft.replaceChildren('// ', usesLeftCount(savedUsesLeft));
    } else {
      savedTokenUsesLeft.textContent = '';
    }
    savedTokenUsesLeft.hidden = !warnAboutUses;
  }

  // The first render happens before anything can fade, so it applies at once.
  renderTokenSource();
  introHidingWaitsForFade = true;

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
    setShowingResult(false);
    result.hidden = true;
    shortUrl.textContent = '';
    shortLinkFull = '';
    errorBox.textContent = message;
    errorBox.hidden = false;
  };

  // A token is worth keeping once the server has accepted it, and worth
  // dropping the moment its last use is spent.
  // Deferred by exactly the fade: applying it sooner would take the token
  // field out of the form card while that card is still on screen fading out.
  const applyTokenOutcome = (tokenValue, usesLeft) => {
    setTimeout(() => {
      if (usesLeft === 0) {
        forgetToken();
      } else {
        rememberToken(tokenValue, usesLeft);
      }

      if (typeof usesLeft !== 'number' || usesLeft > lowUsesThreshold) return;
      if (usesLeft === 0) {
        // The em dash belongs to the note; only the address is a link.
        const mailto = document.createElement('a');
        mailto.href = 'mailto:zibs@dinubarbu.com';
        mailto.textContent = 'zibs@dinubarbu.com';
        usesLeftNote.replaceChildren(
          "// you've consumed your zib token — get a new one: ",
          mailto,
        );
      } else {
        // Only the count carries the accent, so the sentence around it stays
        // as quiet as the rest of the terminal.
        usesLeftNote.replaceChildren('// ', usesLeftCount(usesLeft), ' with your saved zib token');
      }
      usesLeftNote.hidden = false;
    }, fadeDurationMs(form))
  };

  form.addEventListener('submit', async (event) => {
    event.preventDefault();
    errorBox.hidden = true;
    result.hidden = true;
    shortUrl.textContent = '';
    shortLinkFull = '';

    usesLeftNote.hidden = true;
    usesLeftNote.textContent = '';

    const url = destination.value.trim();
    const tokenValue = savedToken || token.value.trim();
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
        setShowingResult(true);
        applyTokenOutcome(tokenValue, body.usesLeft);
        const link = new URL('/' + body.code, window.location.origin);
        shortLinkFull = link.toString();
        shortUrl.textContent = link.host + link.pathname;
        clearTimeout(copyConfirmTimer);
        copyConfirm.hidden = true;
        copyButton.hidden = false;
        // reset() blanks the token field too. Putting the submitted token
        // straight back keeps the card unchanged while it fades out;
        // applyTokenOutcome re-renders the field once the fade is over.
        form.reset();
        token.value = tokenValue;
        await fadeOut(form);
        fadeIn(result);
        copyButton.focus({ preventScroll: true });
      } else if (response.status === 401) {
        // The saved token is no longer usable; drop it so the field comes
        // back and a fresh token can be entered.
        forgetToken();
        showError('The token was rejected — it may have been revoked or run out of uses. Ask for a fresh one.');
      } else if (response.status === 400) {
        // The token was spent before the body was validated, so it is known
        // good even though this request failed — and one use poorer than the
        // count we had, which the failed response does not carry.
        applyTokenOutcome(
          tokenValue,
          savedUsesLeft === null ? undefined : Math.max(savedUsesLeft - 1, 0),
        );
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
    setShowingResult(false);
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
