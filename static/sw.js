/* Synapse service worker — Web Push delivery only. No offline cache. */

self.addEventListener('install', function() {
    self.skipWaiting();
});

self.addEventListener('activate', function(event) {
    event.waitUntil(self.clients.claim());
});

self.addEventListener('push', function(event) {
    var payload = {};
    if (event.data) {
        try {
            payload = event.data.json();
        } catch (err) {
            payload = { title: 'Synapse', body: event.data.text() };
        }
    }
    if (!payload || typeof payload !== 'object') payload = {};

    var title = payload.title || 'Synapse';
    var category = payload.category || '';
    var options = {
        body: payload.body || '',
        icon: payload.icon || '/favicon.ico',
        badge: payload.badge || '/favicon.ico',
        tag: payload.tag || category || 'synapse',
        data: {
            url: payload.url || '/',
            category: category
        }
    };

    event.waitUntil(self.registration.showNotification(title, options));
});

self.addEventListener('notificationclick', function(event) {
    event.notification.close();

    var data = event.notification.data || {};
    var target = data.url || '/';

    event.waitUntil(
        self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then(function(list) {
            for (var i = 0; i < list.length; i++) {
                var client = list[i];
                try {
                    var url = new URL(client.url);
                    if (client.url === target || (url.pathname + url.search) === target) {
                        if ('focus' in client) return client.focus();
                    }
                } catch (err) {
                    /* ignore malformed client URLs */
                }
            }
            if (self.clients.openWindow) return self.clients.openWindow(target);
        })
    );
});

self.addEventListener('pushsubscriptionchange', function() {
    // Re-subscribing needs the VAPID key and an authenticated request, which
    // only the page can provide. The user re-enables push from the bell menu.
    console.warn('Synapse: push subscription changed — re-enable browser notifications in the app.');
});
