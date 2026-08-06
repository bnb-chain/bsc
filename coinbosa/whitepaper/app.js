  (function () {
    var root = document.documentElement, KEY = "coinbosa-wp-theme";
    var toggle = document.getElementById("theme-toggle");
    var moon = document.getElementById("ic-moon"), sun = document.getElementById("ic-sun");
    var mq = window.matchMedia ? window.matchMedia("(prefers-color-scheme: dark)") : null;
    function resolved() { var s=null; try{s=localStorage.getItem(KEY);}catch(e){} if(s==="light"||s==="dark")return s; return (mq&&mq.matches)?"dark":"light"; }
    function paint(t){ var d=t==="dark"; sun.style.display=d?"block":"none"; moon.style.display=d?"none":"block"; toggle.setAttribute("aria-pressed", d?"true":"false"); }
    function apply(t,p){ root.setAttribute("data-theme",t); if(p){try{localStorage.setItem(KEY,t);}catch(e){}} paint(t); }
    (function(){ var s=null; try{s=localStorage.getItem(KEY);}catch(e){} if(s==="light"||s==="dark")apply(s,false); else paint(resolved()); })();
    toggle.addEventListener("click", function(){ apply(resolved()==="dark"?"light":"dark", true); });
    if (mq){ var l=function(){ var s=null; try{s=localStorage.getItem(KEY);}catch(e){} if(s!=="light"&&s!=="dark")paint(resolved()); };
      if(mq.addEventListener)mq.addEventListener("change",l); else if(mq.addListener)mq.addListener(l); }

    document.getElementById("print-btn").addEventListener("click", function(){ window.print(); });

    // Sommaire mobile
    var toc = document.getElementById("toc"), tocBtn = document.getElementById("toc-toggle");
    tocBtn.addEventListener("click", function(){
      var open = toc.getAttribute("data-open")==="true";
      toc.setAttribute("data-open", open?"false":"true");
      tocBtn.setAttribute("aria-expanded", open?"false":"true");
    });
    // referme apres clic sur un lien (mobile)
    toc.addEventListener("click", function(e){ if(e.target.closest("a") && window.matchMedia("(max-width:860px)").matches){ toc.setAttribute("data-open","false"); tocBtn.setAttribute("aria-expanded","false"); } });

    // Suivi de lecture (scroll-spy)
    var links = {}, secs = [];
    toc.querySelectorAll('a[href^="#sec-"]').forEach(function(a){ links[a.getAttribute("href").slice(1)] = a; });
    Object.keys(links).forEach(function(id){ var el=document.getElementById(id); if(el)secs.push(el); });
    var current = null;
    function setCurrent(id){ if(current===id)return; if(current&&links[current])links[current].removeAttribute("aria-current"); current=id; if(links[id])links[id].setAttribute("aria-current","true"); }
    if ("IntersectionObserver" in window && secs.length){
      var io = new IntersectionObserver(function(entries){
        entries.forEach(function(en){ if(en.isIntersecting) setCurrent(en.target.id); });
      }, { rootMargin: "-15% 0px -70% 0px", threshold: 0 });
      secs.forEach(function(s){ io.observe(s); });
    }
  })();
