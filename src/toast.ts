// Toast notification system

export function toast(msg: string, type?: string): void {
    type = type || 'info';
    var container = document.getElementById('toast-container');
    if (!container) {
        container = document.createElement('div');
        container.id = 'toast-container';
        container.className = 'toast-container';
        document.body.appendChild(container);
    }
    var el = document.createElement('div');
    el.className = 'toast-msg toast-' + type;
    el.textContent = msg;
    el.setAttribute('role', 'alert');
    el.setAttribute('aria-live', 'assertive');
    container.appendChild(el);

    // Pause auto-dismiss on hover
    var timer = setTimeout(function() { el.remove(); }, 4000);
    el.addEventListener('mouseenter', function() { clearTimeout(timer); });
    el.addEventListener('mouseleave', function() {
        timer = setTimeout(function() { el.remove(); }, 4000);
    });
}

// Make globally accessible for modules that reference toast/setLoading directly
window.toast = toast;
window.setLoading = setLoading;

export function setLoading(btnId: string, loading: boolean): void {
    var btn = document.getElementById(btnId) as HTMLButtonElement | null;
    if (!btn) return;
    if (loading) {
        btn.dataset.origHtml = btn.innerHTML;
        btn.disabled = true;
        btn.innerHTML = '<span class="spinner-sm"></span> ' + (btn.dataset.loadingText || btn.textContent!.trim());
    } else {
        btn.disabled = false;
        if (btn.dataset.origHtml) btn.innerHTML = btn.dataset.origHtml;
    }
}
