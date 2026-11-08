// My Jobs page (TEL-35): lists the user's jobs from GET /api/jobs with a
// coloured badge for each status, and refreshes while jobs are still active.
document.addEventListener("DOMContentLoaded", () => {
  const search = document.getElementById("job-search");
  const statusFilter = document.getElementById("status-filter");
  const count = document.getElementById("job-count");
  const message = document.getElementById("jobs-message");
  const table = document.getElementById("jobs-table");
  const body = document.getElementById("jobs-body");

  // The five job states from internal/model/job.go.
  const statusLabels = {
    queued: "Queued",
    running: "Running",
    completed: "Completed",
    failed: "Failed",
    cancelled: "Cancelled"
  };
  // Jobs in these states will not change any more, so no need to refresh.
  const finished = new Set(["completed", "failed", "cancelled"]);

  let allJobs = [];

    // ---------- small helpers ----------

  function escapeHTML(value) {
    const span = document.createElement("span");
    span.textContent = value ?? "";
    return span.innerHTML;
  }

  function formatDate(value) {
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? "—" : date.toLocaleString();
  }

  function capitalise(text) {
    return text ? text.charAt(0).toUpperCase() + text.slice(1) : "";
  }

  // <span class="badge running">Running</span>
  function statusBadge(status) {
    const known = Object.hasOwn(statusLabels, status);
    const cssClass = known ? status : "unknown";
    const text = known ? statusLabels[status] : capitalise(status);
    return `<span class="badge ${cssClass}">${escapeHTML(text)}</span>`;
  }

    // ---------- drawing the table ----------

  function matchesFilters(job) {
    const wantedStatus = statusFilter.value;
    if (wantedStatus && job.status !== wantedStatus) return false;

    const text = search.value.trim().toLowerCase();
    if (!text) return true;
    return (job.name || "").toLowerCase().includes(text) ||
           job.id.toLowerCase().includes(text);
  }

  function render() {
    const jobs = allJobs.filter(matchesFilters);
    body.replaceChildren();

    if (allJobs.length === 0) {
      showMessage("No jobs submitted yet.");
      count.textContent = "";
      return;
    }
    count.textContent = `Showing ${jobs.length} of ${allJobs.length} jobs`;
    if (jobs.length === 0) {
      showMessage("No jobs match your filters.");
      return;
    }

    jobs.forEach((job) => {
      const detailsURL = `/user/job-details?id=${encodeURIComponent(job.id)}`;
      const isDone = job.status === "completed";
      const actionURL = isDone ? `/user/results?id=${encodeURIComponent(job.id)}` : detailsURL;

      const row = document.createElement("tr");
      row.innerHTML = `
        <td><a href="${detailsURL}" title="${escapeHTML(job.id)}">${escapeHTML(job.id.slice(0, 8))}</a></td>
        <td>${escapeHTML(job.name || "Unnamed job")}</td>
        <td>${statusBadge(job.status)}</td>
        <td>${escapeHTML(capitalise(job.priority || "normal"))}</td>
        <td>${escapeHTML(formatDate(job.created_at))}</td>
        <td><a href="${actionURL}">${isDone ? "Results" : "View"}</a></td>
      `;
      body.appendChild(row);
    });
    table.hidden = false;
    message.hidden = true;
  }

  function showMessage(text) {
    table.hidden = true;
    message.hidden = false;
    message.textContent = text;
  }

    // ---------- loading from the API ----------

  async function loadJobs() {
    try {
      const response = await fetch("/api/jobs");
      const data = await response.json();
      if (!response.ok) throw new Error(data.error || "Unable to load jobs.");
      allJobs = data;
      render();

      // Refresh every 5 seconds while any job is still queued or running.
      if (allJobs.some((job) => !finished.has(job.status))) {
        setTimeout(loadJobs, 5000);
      }
    } catch (error) {
      showMessage(error.message);
      setTimeout(loadJobs, 5000);
    }
  }

  // Re-draw (without reloading) when the user types or picks a status.
  search.addEventListener("input", render);
  statusFilter.addEventListener("change", render);

  loadJobs();
});

