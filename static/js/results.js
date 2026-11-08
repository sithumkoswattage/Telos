// Results page (TEL-80): shows the word count result of a completed job.
//
// /user/results?id=<job id>  shows that job.
// /user/results              shows your newest completed job.
document.addEventListener("DOMContentLoaded", () => {
  const message = document.getElementById("results-message");
  const panel = document.getElementById("results-panel");

  function show(id, text) {
    document.getElementById(id).textContent = text;
  }

  function showMessage(text) {
    panel.hidden = true;
    message.hidden = false;
    message.textContent = text;
  }

  async function getJSON(url) {
    const response = await fetch(url);
    const data = await response.json();
    if (!response.ok) throw new Error(data.error || "Unable to load the result.");
    return data;
  }

  // Which job? The one in the address, or else the newest completed one.
  async function findJobID() {
    const id = new URLSearchParams(window.location.search).get("id");
    if (id) return id;
    const jobs = await getJSON("/api/jobs"); // newest first
    const done = jobs.find((job) => job.status === "completed");
    return done ? done.id : null;
  }

  async function load() {
    try {
      const id = await findJobID();
      if (!id) {
        showMessage("No completed jobs yet. Submit a job and wait until My Jobs shows it as Completed.");
        return;
      }

      const job = await getJSON(`/api/jobs/${encodeURIComponent(id)}`);
      if (job.status !== "completed" || !job.result) {
        showMessage(`This job is ${job.status}. The result appears here once it is completed.`);
        // Still working? Check again in 3 seconds.
        if (job.status === "queued" || job.status === "running") setTimeout(load, 3000);
        return;
      }

      show("result-job", `${job.name} (${job.id.slice(0, 8)})`);
      show("result-priority", job.priority.charAt(0).toUpperCase() + job.priority.slice(1));
      show("result-finished", new Date(job.completed_at).toLocaleString());
      show("result-words", job.result.words);
      show("result-lines", job.result.lines);
      show("result-characters", job.result.characters);
      message.hidden = true;
      panel.hidden = false;
    } catch (error) {
      showMessage(error.message);
    }
  }

  load();
});