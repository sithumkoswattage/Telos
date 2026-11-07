
document.addEventListener("DOMContentLoaded", () => {
  const form = document.getElementById("submit-job-form");

  if (!form) return;

  // Form elements
  const jobType = document.getElementById("job-type");
  const jobText = document.getElementById("job-text");
  const jobFile = document.getElementById("job-file");

  const textSection = document.getElementById("text-input-section");
  const fileSection = document.getElementById("file-input-section");

  const fileNameDisplay = document.getElementById("selected-file-name");
  const removeFileButton = document.getElementById("remove-selected-file");

  const errorMessage = document.getElementById("tj-error");
  const submitMessage = document.getElementById("tj-submit-message");

  const dropArea = document.getElementById("tj-drop-area");

  const submitButton = document.getElementById("submit-job-button");

  // Store uploaded file information
  let uploadedFileContent = "";
  let uploadedFileName = "";
  let activeFileReader = null;

  function resetSelectedFile(clearMessages = true, clearInput = true) {
    if (activeFileReader) {
      activeFileReader.abort();
      activeFileReader = null;
    }

    if (clearInput) {
      jobFile.value = "";
    }
    uploadedFileContent = "";
    uploadedFileName = "";
    fileNameDisplay.textContent = "No file selected";
    removeFileButton.hidden = true;

    if (clearMessages) {
      errorMessage.textContent = "";
      submitMessage.textContent = "";
    }
  }

  // Get selected priority
  function getPriority() {
    return document.querySelector(
      'input[name="priority"]:checked'
    )?.value || "normal";
  }

  // Get selected input method
  function getInputMethod() {
    return document.querySelector(
      'input[name="input-method"]:checked'
    )?.value || "text";
  }

  // Switch between text input and file upload
  document.querySelectorAll('input[name="input-method"]').forEach((radio) => {
    radio.addEventListener("change", () => {
      const method = getInputMethod();

      textSection.hidden = method !== "text";
      fileSection.hidden = method !== "file";

      document.querySelectorAll(".tj-method-option").forEach((option) => {
        option.classList.toggle(
          "selected",
          option.querySelector("input").checked
        );
      });

      errorMessage.textContent = "";
      submitMessage.textContent = "";
    });
  });

  // Update priority styling
  document.querySelectorAll('input[name="priority"]').forEach((radio) => {
    radio.addEventListener("change", () => {
      document.querySelectorAll(".tj-priority-option").forEach((option) => {
        option.classList.toggle(
          "selected",
          option.querySelector("input").checked
        );
      });
    });
  });

  // Clear error message on input
  jobText.addEventListener("input", () => {
  errorMessage.textContent = "";
});

  // Read uploaded text file
  function readTextFile(file) {
    if (!file) return;

    resetSelectedFile(false, false);

    if (!file.name.toLowerCase().endsWith(".txt")) {
      errorMessage.textContent = "Please select a .txt file.";
      jobFile.value = "";
      return;
    }

    fileNameDisplay.textContent = file.name;
    removeFileButton.hidden = false;

    const reader = new FileReader();
    activeFileReader = reader;

    reader.onload = () => {
      if (activeFileReader !== reader) return;

      uploadedFileContent = String(reader.result || "");
      uploadedFileName = file.name;

      errorMessage.textContent = "";
      activeFileReader = null;
    };

    reader.onerror = () => {
      if (activeFileReader !== reader) return;

      errorMessage.textContent =
        "Unable to read this file. Please try again.";

      uploadedFileContent = "";
      uploadedFileName = "";
      jobFile.value = "";
      fileNameDisplay.textContent = "No file selected";
      removeFileButton.hidden = true;
      activeFileReader = null;
    };

    reader.readAsText(file);
  }

  removeFileButton.addEventListener("click", () => {
    resetSelectedFile();
  });

  // File selection
  jobFile.addEventListener("change", () => {
    const file = jobFile.files[0];
    readTextFile(file);
  });

  // Drag and drop
  ["dragenter", "dragover"].forEach((eventName) => {
    dropArea.addEventListener(eventName, (event) => {
      event.preventDefault();
      dropArea.classList.add("dragging");
    });
  });

  ["dragleave", "drop"].forEach((eventName) => {
    dropArea.addEventListener(eventName, (event) => {
      event.preventDefault();
      dropArea.classList.remove("dragging");
    });
  });

  dropArea.addEventListener("drop", (event) => {
    event.preventDefault();

    const file = event.dataTransfer.files[0];

    if (!file) return;

    const transfer = new DataTransfer();
    transfer.items.add(file);
    jobFile.files = transfer.files;

    readTextFile(file);
  });

  // Validate form
  function validateForm() {
    if (!jobType.value) {
      errorMessage.textContent = "Please choose a job type.";
      jobType.focus();
      return false;
    }

    if (getInputMethod() === "text") {
      if (!jobText.value.trim()) {
        errorMessage.textContent = "Please enter some text to process.";
        jobText.focus();
        return false;
      }
    } else {
      if (!jobFile.files.length || !uploadedFileName) {
        errorMessage.textContent = "Please upload a .txt file.";
        return false;
      }

      if (!uploadedFileContent.trim()) {
        errorMessage.textContent =
          "The selected file is empty or has not finished loading.";
        return false;
      }
    }

    errorMessage.textContent = "";
    return true;
  }

  async function submitJob(jobData) {
    const response = await fetch("/api/jobs", {
      method: "POST",
      headers: {
        "Content-Type": "application/json"
      },
      body: JSON.stringify({
        type: jobData.type,
        priority: jobData.priority,
        input_method: jobData.inputMethod,
        content: jobData.input,
        ...(jobData.fileName ? { file_name: jobData.fileName } : {})
      })
    });

    let responseData = {};
    try {
      responseData = await response.json();
    } catch (error) {
      if (!response.ok) {
        throw new Error("The server returned an invalid error response.");
      }
    }

    if (!response.ok) {
      throw new Error(responseData.error || "Unable to submit the job.");
    }

    return responseData;
  }

  form.addEventListener("submit", async (event) => {
    event.preventDefault();

    submitMessage.textContent = "";

    if (!validateForm()) return;

    const method = getInputMethod();

    const jobData = {
      type: jobType.value,
      priority: getPriority(),
      inputMethod: method,
      input: method === "text"
        ? jobText.value.trim()
        : uploadedFileContent,
      fileName: method === "file" ? uploadedFileName : null
    };

    submitButton.disabled = true;
    submitButton.textContent = "Submitting...";

    try {
      const response = await submitJob(jobData);
      submitMessage.textContent =
        `Job ${response.id} is ${response.status}.`;
      errorMessage.textContent = "";
    } catch (error) {
      errorMessage.textContent = error.message;
      submitMessage.textContent = "";
    } finally {
      submitButton.disabled = false;
      submitButton.textContent = "Submit Job →";
    }
  });

});
