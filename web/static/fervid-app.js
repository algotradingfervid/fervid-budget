(function () {
  function toggleProject(button) {
    var projectID = button.getAttribute("data-project-toggle");
    var table = button.closest("table");
    if (!projectID || !table) return;

    var isExpanded = button.getAttribute("aria-expanded") !== "false";
    var rows = table.querySelectorAll('[data-project-row="' + projectID + '"]');
    for (var i = 0; i < rows.length; i += 1) {
      rows[i].hidden = isExpanded;
    }

    button.setAttribute("aria-expanded", isExpanded ? "false" : "true");
    var projectRow = button.closest(".project-row");
    if (projectRow) {
      projectRow.classList.toggle("is-collapsed", isExpanded);
    }
  }

  document.addEventListener("click", function (event) {
    var button = event.target.closest("[data-project-toggle]");
    if (!button) return;
    toggleProject(button);
  });
})();
