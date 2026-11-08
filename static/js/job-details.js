// Job Details page: shows one job and how far it has got.
//
// /user/job-details?id=<job id>  shows that job.
// /user/job-details              shows your newest job.
document.addEventListener("DOMContentLoaded", () => {
  const message = document.getElementById("job-message");
  const content = document.getElementById("job-content");
  let jobID = null; // remembered, so refreshing keeps showing the same job

  function show(id, text) {
    document.getElementById(id).textContent = text;
  }

  function capitalise(text) {
    return text ? text.charAt(0).toUpperCase() + text.slice(1) : "";
  }

  // count(1, "line") -> "1 line", count(3, "line") -> "3 lines"
  function count(n, word) {
    return `${n} ${word}${n === 1 ? "" : "s"}`;
  }

  function formatDate(value) {
    if (!value) return "—";
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
  }

  function showMessage(text) {
    content.hidden = true;
    message.textContent = text;
  }

  async function getJSON(url) {
    const response = await fetch(url);
    const data = await response.json();
    if (!response.ok) throw new Error(data.error || "Unable to load the job.");
    return data;
  }

  // Which job? The one in the address, or else the newest one.
  async function findJobID() {
    const id = new URLSearchParams(window.location.search).get("id");
    if (id) return id;
    const jobs = await getJSON("/api/jobs"); // newest first
    return jobs.length > 0 ? jobs[0].id : null;
  }

  // Mark a step green (done) or grey (not yet).
  function setStep(id, done) {
    document.getElementById(id).classList.toggle("done", done);
  }

  function render(job) {
    // Badge colour: these classes exist in app.css.
    const pillClass = { queued: "queued", running: "processing", completed: "completed" }[job.status] || "failed";
    const badge = document.getElementById("job-status");
    badge.className = `pill big ${pillClass}`;
    badge.textContent = capitalise(job.status);

    show("job-title", `${job.name} (${job.id.slice(0, 8)})`);
    show("job-type", job.type);
    show("job-priority", capitalise(job.priority));
    show("job-queue", job.queue);
    show("job-created", formatDate(job.created_at));
    show("job-updated", formatDate(job.updated_at));
    message.textContent = `Submitted ${formatDate(job.created_at)}`;

    // Progress steps.
    const finished = ["completed", "failed", "cancelled"].includes(job.status);
    setStep("step-submitted", true);
    setStep("step-started", Boolean(job.started_at));
    setStep("step-finished", finished);

    const notes = {
      queued: "Waiting in the queue.",
      running: job.started_at ? `Started ${formatDate(job.started_at)}. Waiting for a worker to finish it.` : "Starting...",
      completed: `Finished ${formatDate(job.completed_at)}.`,
      failed: `Failed ${formatDate(job.completed_at)}.`,
      cancelled: "This job was cancelled."
    };
    show("job-progress-note", notes[job.status] || "");

    // Error (failed jobs only).
    const error = document.getElementById("job-error");
    error.hidden = !(job.status === "failed" && job.error);
    error.textContent = `Error: ${job.error || ""}`;

    // Result summary (completed jobs only).
    const result = document.getElementById("job-result");
    result.hidden = !(job.status === "completed" && job.result);
    if (!result.hidden) {
      show("job-result-text", [
        count(job.result.words, "word"),
        count(job.result.lines, "line"),
        count(job.result.characters, "character")
      ].join(", "));
      document.getElementById("job-result-link").href =
        `/user/results?id=${encodeURIComponent(job.id)}`;
    }

    content.hidden = false;
  }

  async function load() {
    try {
      jobID = jobID || await findJobID();
      if (!jobID) {
        showMessage("You have not submitted any jobs yet.");
        return;
      }
      const job = await getJSON(`/api/jobs/${encodeURIComponent(jobID)}`);
      render(job);

      // Still working? Check again in 3 seconds.
      if (job.status === "queued" || job.status === "running") {
        setTimeout(load, 3000);
      }
    } catch (error) {
      showMessage(error.message);
    }
  }

  load();
});