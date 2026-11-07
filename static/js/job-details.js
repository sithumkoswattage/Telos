document.addEventListener("DOMContentLoaded", () => {
  const id = new URLSearchParams(window.location.search).get("id");
  const message = document.getElementById("job-message");
  const status = document.getElementById("job-status");
  const largeStatus = document.getElementById("job-status-large");
  const error = document.getElementById("job-error");
  let pollTimer;

  function setStatus(value) {
    const className = value === "running" ? "processing"
      : ["queued", "completed", "failed"].includes(value) ? value : "failed";
    [status, largeStatus].forEach((element) => {
      element.className = `pill ${element === largeStatus ? "big " : ""}${className}`;
      element.textContent = value ? value.charAt(0).toUpperCase() + value.slice(1) : "Unknown";
    });
  }

  function formatDate(value) {
    if (!value) return "—";
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
  }

  function showError(text) {
    message.textContent = text;
    error.hidden = true;
  }

  async function loadJob() {
    if (!id) {
      showError("A job ID is required to view job details.");
      return;
    }

    try {
      const response = await fetch(`/api/jobs/${encodeURIComponent(id)}`);
      const data = await response.json();
      if (!response.ok) throw new Error(data.error || "Unable to load job.");

      document.getElementById("job-title").textContent = `${data.id} - ${data.name}`;
      document.getElementById("job-type").textContent = data.type;
      document.getElementById("job-priority").textContent = data.priority;
      document.getElementById("job-queue").textContent = data.queue;
      document.getElementById("job-created").textContent = formatDate(data.created_at);
      document.getElementById("job-updated").textContent = formatDate(data.updated_at);
      setStatus(data.status);
      message.textContent = `Submitted ${formatDate(data.created_at)}`;
      if (data.status === "failed" && data.error) {
        error.hidden = false;
        error.textContent = `Error: ${data.error}`;
      } else {
        error.hidden = true;
      }

      if (data.status === "queued" || data.status === "running") {
        pollTimer = window.setTimeout(loadJob, 5000);
      }
    } catch (loadError) {
      showError(loadError.message);
    }
  }

  loadJob();
});
