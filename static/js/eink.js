// E-ink mode activation for login/setup pages (no Vite bundle).
// Reads ?eink=1 URL param and/or eink cookie; applies html.eink-mode class.
(function () {
    function getCookie(name) {
        var m = document.cookie.match(new RegExp('(?:^|; )' + name + '=([^;]*)'));
        return m ? decodeURIComponent(m[1]) : null;
    }
    function setCookie(name, value, days) {
        var d = new Date();
        d.setTime(d.getTime() + days * 86400000);
        document.cookie = name + '=' + encodeURIComponent(value) + '; path=/; expires=' + d.toUTCString() + '; SameSite=Lax';
    }
    var params = new URLSearchParams(window.location.search);
    var active = false;
    if (params.get('eink') !== null) {
        setCookie('eink', '1', 365);
        active = true;
    } else if (getCookie('eink') === '1') {
        active = true;
    }
    if (active) document.documentElement.classList.add('eink-mode');
})();
