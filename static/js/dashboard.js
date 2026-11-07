document.addEventListener("DOMContentLoaded", () => {
  const total = document.getElementById("total-jobs");
  const counts = {
    queued: document.getElementById("queued-jobs"),
    running: document.getElementById("processing-jobs"),
    completed: document.getElementById("completed-jobs"),
    failed: document.getElementById("failed-jobs")
  };
  const message = document.getElementById("jobs-message");
  const table = document.getElementById("jobs-table");
  const body = document.getElementById("jobs-body");
  let pollTimer;

  const terminalStatuses = new Set(["completed", "failed", "cancelled"]);

  function label(status) {
    return status.charAt(0).toUpperCase() + status.slice(1);
  }

  function formatDate(value) {
    if (!value) return "—";
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
  }

  function statusClass(status) {
    if (status === "queued" || status === "running") return "processing";
    return ["completed", "failed"].includes(status) ? status : "failed";
  }

  function render(jobs) {
    const statusCounts = {queued: 0, running: 0, completed: 0, failed: 0};
    jobs.forEach((job) => {
      if (Object.hasOwn(statusCounts, job.status)) statusCounts[job.status] += 1;
    });
    total.textContent = jobs.length;
    Object.entries(counts).forEach(([status, element]) => {
      element.textContent = statusCounts[status];
    });

    body.replaceChildren();
    if (jobs.length === 0) {
      table.hidden = true;
      message.hidden = false;
      message.textContent = "No jobs submitted yet.";
      return;
    }

    jobs.forEach((job) => {
      const row = document.createElement("tr");
      const detailsURL = `/user/job-details?id=${encodeURIComponent(job.id)}`;
      const actionURL = job.status === "completed"
        ? `/user/results?id=${encodeURIComponent(job.id)}`
        : detailsURL;
      const actionLabel = job.status === "completed" ? "Results" : "View";
      row.innerHTML = `
        <td>${escapeHTML(job.id)}</td>
        <td>${escapeHTML(job.name || "Unnamed job")}</td>
        <td><span class="badge ${statusClass(job.status)}">${escapeHTML(label(job.status))}</span></td>
        <td>${escapeHTML(label(job.priority || "normal"))}</td>
        <td>${escapeHTML(formatDate(job.created_at))}</td>
        <td><a href="${actionURL}">${actionLabel}</a></td>
      `;
      body.appendChild(row);
    });
    table.hidden = false;
    message.hidden = true;
  }

  function escapeHTML(value) {
    const element = document.createElement("span");
    element.textContent = value;
    return element.innerHTML;
  }

  async function loadJobs() {
    try {
      const response = await fetch("/api/jobs");
      const data = await response.json();
      if (!response.ok) throw new Error(data.error || "Unable to load jobs.");
      render(data);
      if (data.some((job) => !terminalStatuses.has(job.status))) {
        pollTimer = window.setTimeout(loadJobs, 5000);
      }
    } catch (error) {
      table.hidden = true;
      message.hidden = false;
      message.textContent = error.message;
      pollTimer = window.setTimeout(loadJobs, 5000);
    }
  }

  loadJobs();
});
