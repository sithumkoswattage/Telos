// Click-to-edit (inline-editable) components (TEL-81).
//
// Turns a piece of text on the page into something you can click to edit.
// The change lives only in the page: nothing is sent to the server. A page
// that wants to keep the value listens for the "inline-edit:save" event.
//
// ---------- How to use ----------
// 1. Load the script on the page:   <script src="/static/js/inline-edit.js"></script>
// 2. Mark the element:
//
//      <h2 data-inline-edit="heading" data-label="Job name" data-required>Weekly report</h2>
//      <span data-inline-edit="number" data-label="Max retries" data-min="0" data-max="10">3</span>
//
//    data-inline-edit   text | heading | label | number   (anything else counts as text)
//    data-label         what the value is called; used for screen readers and error messages
//    data-name          optional id for your own code; sent back in the events
//    data-required      present = an empty value cannot be saved
//    data-placeholder   shown when the value is empty (default "Click to edit")
//    data-min, data-max, data-step    number only
//
//    Put the attribute on a <span>, <p>, <div> or a heading, not on a <label for="...">.
//
// 3. Elements added to the page later (for example by a script that builds a
//    table) are picked up by calling:   InlineEdit.init(parentElement)
//
// ---------- How it behaves ----------
// - Click the value, or Tab to it and press Enter or Space, to start editing.
//   Selecting a table row never starts editing.
// - Enter or the Save button saves. Escape or the Cancel button throws the
//   edit away and restores the original value.
// - A required field cannot be saved empty. A number must be a number, and
//   inside min / max / step if those are set. The message shows under the field.
// - Clicking or tabbing away with unsaved changes does NOT save or discard
//   them: the field stays open and shows "Unsaved changes". With no changes,
//   it just closes. Closing the tab with unsaved changes asks for confirmation.
//
// ---------- Events (fired on the element you marked, they bubble) ----------
//   inline-edit:save    event.detail = { name, label, value, previous }
//                       only fires when the value really changed
//   inline-edit:cancel  event.detail = { name, label, value }
//   (number fields give value / previous as numbers, other types as strings)
//
// ---------- Reading or setting the value from code ----------
//   element.inlineEdit.getValue()
//   element.inlineEdit.setValue(newValue)   // e.g. to put an old value back
//   element.inlineEdit.isEditing(), .isDirty(), .open(), .cancel()
(function () {
  "use strict";

  const SELECTOR = "[data-inline-edit]";
  const KNOWN_TYPES = ["text", "heading", "label", "number"];
  const PENCIL =
    '<svg viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" ' +
    'stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" ' +
    'focusable="false"><path d="M11 2.5l2.5 2.5L5.5 13l-3 .5.5-3z"/></svg>';

  let nextId = 0;

  // Editors that have unsaved changes. Used to warn before leaving the page.
  const dirtyHosts = new Set();

  function warnBeforeLeaving(event) {
    const stillOnPage = Array.from(dirtyHosts).some((host) => host.isConnected);
    if (!stillOnPage) return;
    event.preventDefault();
    event.returnValue = "";
  }

  function markDirty(host, isDirty) {
    if (isDirty) {
      if (dirtyHosts.size === 0) window.addEventListener("beforeunload", warnBeforeLeaving);
      dirtyHosts.add(host);
    } else {
      dirtyHosts.delete(host);
      if (dirtyHosts.size === 0) window.removeEventListener("beforeunload", warnBeforeLeaving);
    }
  }

  function make(tag, className) {
    const node = document.createElement(tag);
    if (className) node.className = className;
    return node;
  }

  // "12" -> 12, missing or not a number -> null
  function readNumber(text) {
    if (text === undefined || text === "") return null;
    const n = Number(text);
    return Number.isFinite(n) ? n : null;
  }

  function capitalise(text) {
    return text.charAt(0).toUpperCase() + text.slice(1);
  }

  function setUp(host) {
    host.dataset.ieReady = "true";

    const kind = KNOWN_TYPES.includes(host.dataset.inlineEdit) ? host.dataset.inlineEdit : "text";
    const isNumber = kind === "number";
    const id = ++nextId;
    const label = host.dataset.label || host.dataset.name || "value";
    const name = host.dataset.name || "";
    const required = host.hasAttribute("data-required");
    const placeholder = host.dataset.placeholder || "Click to edit";
    const min = isNumber ? readNumber(host.dataset.min) : null;
    const max = isNumber ? readNumber(host.dataset.max) : null;
    const step = isNumber ? readNumber(host.dataset.step) : null;

    let committed = host.textContent.trim(); // the saved value, as text
    let editing = false;
    let dirty = false; // the field differs from the saved value
    let flagged = false; // focus left while dirty, so show the hint

    // ----- build the pieces -----
    host.textContent = "";

    const root = make("span", "ie");
    root.dataset.type = kind;
    root.dataset.state = "view";

    // View: the value, as a real button so the keyboard can reach it.
    const display = make("button", "ie-display");
    display.type = "button";
    const valueText = make("span", "ie-value");
    const pencil = make("span", "ie-pencil");
    pencil.setAttribute("aria-hidden", "true");
    pencil.innerHTML = PENCIL; // fixed markup written above, no user text in it
    display.append(valueText, pencil);

    // Not shown on screen. Read out after the button's name by screen readers.
    const description = make("span");
    description.id = "ie-desc-" + id;
    description.hidden = true;
    description.textContent = "Edit " + label;
    display.setAttribute("aria-describedby", description.id);

    // Edit: the input with Save and Cancel.
    const group = make("span", "ie-form");
    group.hidden = true;
    group.setAttribute("role", "group");
    group.setAttribute("aria-label", "Edit " + label);

    const input = make("input", "ie-input");
    input.type = isNumber ? "number" : "text";
    if (isNumber) input.inputMode = "decimal";
    if (min !== null) input.min = String(min);
    if (max !== null) input.max = String(max);
    if (step !== null) input.step = String(step);
    input.setAttribute("aria-label", label);
    input.autocomplete = "off";

    const saveButton = make("button", "btn small ie-save");
    saveButton.type = "button";
    saveButton.textContent = "Save";

    const cancelButton = make("button", "btn ghost small ie-cancel");
    cancelButton.type = "button";
    cancelButton.textContent = "Cancel";

    const error = make("span", "ie-error");
    error.id = "ie-error-" + id;
    error.hidden = true;
    error.setAttribute("role", "alert");

    const hint = make("span", "ie-hint");
    hint.hidden = true;
    hint.setAttribute("aria-live", "polite");
    hint.textContent = "Unsaved changes. Save or Cancel.";

    group.append(input, saveButton, cancelButton, error, hint);
    root.append(display, description, group);
    host.append(root);

    // ----- helpers -----
    // Number fields give numbers back to the page, other types give text.
    function typed(text) {
      return isNumber && readNumber(text) !== null ? Number(text) : text;
    }

    function sameValue(a, b) {
      if (isNumber && a !== "" && b !== "") {
        const x = readNumber(a);
        const y = readNumber(b);
        if (x !== null && y !== null) return x === y;
      }
      return a === b;
    }

    function showValue() {
      valueText.textContent = committed || placeholder;
      valueText.classList.toggle("ie-empty", committed === "");
    }

    function showError(message) {
      error.textContent = message;
      error.hidden = false;
      input.setAttribute("aria-invalid", "true");
      input.setAttribute("aria-describedby", error.id);
    }

    function clearError() {
      error.textContent = "";
      error.hidden = true;
      input.removeAttribute("aria-invalid");
      input.removeAttribute("aria-describedby");
    }

    function updateHint() {
      hint.hidden = !(flagged && dirty);
    }

    function setDirty(value) {
      dirty = value;
      markDirty(host, value);
      updateHint();
    }

    function refreshDirty() {
      setDirty(editing && !sameValue(input.value.trim(), committed));
    }

    // Returns a message if the value cannot be saved, or "" if it is fine.
    function problemWith(text) {
      if (isNumber && input.validity.badInput) return "Enter a valid number.";
      if (text === "") return required ? capitalise(label) + " is required." : "";
      if (!isNumber) return "";

      const n = readNumber(text);
      if (n === null) return "Enter a valid number.";
      if (min !== null && n < min) return "Must be at least " + min + ".";
      if (max !== null && n > max) return "Must be at most " + max + ".";
      if (step !== null && step > 0) {
        const steps = (n - (min !== null ? min : 0)) / step;
        if (Math.abs(steps - Math.round(steps)) > 1e-9) return "Must go up in steps of " + step + ".";
      }
      return "";
    }

    function send(type, detail) {
      host.dispatchEvent(
        new CustomEvent(type, { bubbles: true, detail: Object.assign({ name, label }, detail) })
      );
    }

    // ----- the four moves -----
    function open() {
      if (editing) return;
      editing = true;
      flagged = false;
      input.value = committed;
      clearError();
      root.dataset.state = "edit";
      display.hidden = true;
      group.hidden = false;
      setDirty(false);
      input.focus();
      input.select();
    }

    function close(giveFocusBack) {
      editing = false;
      flagged = false;
      clearError();
      setDirty(false);
      root.dataset.state = "view";
      group.hidden = true;
      display.hidden = false;
      if (giveFocusBack) display.focus();
    }

    function save() {
      const text = input.value.trim();
      const problem = problemWith(text);
      if (problem) {
        showError(problem);
        input.focus();
        return;
      }

      const next = isNumber && text !== "" ? String(Number(text)) : text;
      const previous = committed;
      const changed = !sameValue(next, previous);

      committed = next;
      showValue();
      close(true);
      if (changed) send("inline-edit:save", { value: typed(next), previous: typed(previous) });
    }

    function cancel() {
      if (!editing) return;
      input.value = committed;
      close(true);
      send("inline-edit:cancel", { value: typed(committed) });
    }

    // ----- wire up the events -----
    display.addEventListener("click", open);
    saveButton.addEventListener("click", save);
    cancelButton.addEventListener("click", cancel);

    input.addEventListener("input", () => {
      if (!error.hidden) clearError();
      refreshDirty();
    });

    input.addEventListener("keydown", (event) => {
      if (event.isComposing) return; // still choosing characters (IME)
      if (event.key === "Enter") {
        event.preventDefault();
        save();
      }
    });

    group.addEventListener("keydown", (event) => {
      if (event.key === "Escape") {
        event.preventDefault();
        event.stopPropagation();
        cancel();
      }
    });

    // Pressing Save or Cancel must not pull focus out of the field first,
    // otherwise some browsers treat it as "leaving the field".
    group.addEventListener("mousedown", (event) => {
      if (event.target.closest("button")) event.preventDefault();
    });

    // Focus left the editor. Wait a tick so we know where it went.
    group.addEventListener("focusout", () => {
      setTimeout(() => {
        if (!editing) return;
        if (group.contains(document.activeElement)) return; // still inside (or the window lost focus)
        if (dirty) {
          flagged = true; // never save or discard silently: stay open and say so
          updateHint();
        } else {
          close(false);
        }
      }, 0);
    });

    showValue();

    host.inlineEdit = {
      getValue: () => typed(committed),
      setValue(value) {
        committed = String(value === null || value === undefined ? "" : value).trim();
        showValue();
        if (editing) {
          input.value = committed;
          clearError();
          setDirty(false);
        }
      },
      isEditing: () => editing,
      isDirty: () => dirty,
      open,
      cancel,
    };
  }

  function init(parent) {
    const scope = parent || document;
    const found = Array.from(scope.querySelectorAll(SELECTOR));
    if (scope.matches && scope.matches(SELECTOR)) found.unshift(scope);
    found.forEach((host) => {
      if (!host.dataset.ieReady) setUp(host);
    });
  }

  window.InlineEdit = { init };

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", () => init());
  } else {
    init();
  }
})();
