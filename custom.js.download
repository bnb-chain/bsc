document.addEventListener("DOMContentLoaded", function() {
    var navLinks = document.querySelectorAll("nav a");

    navLinks.forEach(function(link) {
        if (link.href.startsWith("http") && !link.href.includes(window.location.hostname)) {
            link.setAttribute("target", "_blank");
        }
    });
});