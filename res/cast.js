/*
 * video-listing Copyright (C) 2023 Mook
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as published
 * by the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU Affero General Public License for more details.
 *
 * You should have received a copy of the GNU Affero General Public License
 * along with this program.  If not, see <https://www.gnu.org/licenses/>.
 */

/// <reference types="chromecast-caf-sender" />

function __onGCastApiAvailable(isAvailable, ...details) {
    console.log('Loading cast framework...', isAvailable, details);
    if (isAvailable) {
        CastManager
            .getInstance()
            .initialize()
            .then(() => console.log('initialized'))
            .catch((ex) => console.error('error initializing:', ex));
    }
}

class CastManager {
    /** @type CastManager? */
    static instance = undefined;
    static getInstance() {
        CastManager.instance ??= new CastManager();
        return CastManager.instance;
    }

    get context() {
        return cast.framework.CastContext.getInstance();
    }

    get session() {
        return this.context.getCurrentSession();
    }

    /** @type cast.framework.RemotePlayer? */
    _player;

    get player() {
        return this._player ??= new cast.framework.RemotePlayer();
    }

    /** @type cast.framework.RemotePlayerController? */
    _controller;
    get controller() {
        return this._controller ?? (() => {
            this._controller = new cast.framework.RemotePlayerController(this.player);
            this.registerEventListeners(this._controller, cast.framework.RemotePlayerEventType);
            return this._controller;
        })();
    }

    async initialize() {
        console.debug("Initializing CastManager");
        cast.framework.setLoggerLevel(cast.framework.LoggerLevel.DEBUG);
        this.context.setOptions({
            autoJoinPolicy: chrome.cast.AutoJoinPolicy.ORIGIN_SCOPED,
            receiverApplicationId: chrome.cast.media.DEFAULT_MEDIA_RECEIVER_APP_ID,
            resumeSavedSession: true,
        });
        console.debug('Registering event listeners');
        this.registerEventListeners(this.context, cast.framework.CastContextEventType);
        this.registerEventListeners(this.controller, cast.framework.RemotePlayerEventType);
    }

    registerEventListeners(obj, eventNames) {
        const handlerNames= Object.getOwnPropertyNames(CastManager.prototype)
            .filter(x => x.startsWith('on') && typeof this[x] === 'function');
        for (const eventName of Object.values(eventNames)) {
            const lowerHandlerName = ('on'+eventName).toLowerCase();
            const handlerName = handlerNames.find(x => x.toLowerCase() === lowerHandlerName);
            if (handlerName) {
                console.debug(`registering event handler ${handlerName}`);
                obj.addEventListener(eventName, this[handlerName].bind(this));
            } else {
                obj.addEventListener(eventName, (event) => {
                    console.debug(eventName, event);
                });
            }
        }
    }

    /**
     * Load give media into the session.
     * @param {string} url The URL to load from. 
     */
    async loadMedia(url) {
        try {
            console.debug(`Loading media ${ url }...`);
            let mediaInfo = new chrome.cast.media.MediaInfo(url, "application/dash+xml");
            console.debug('Using media info', mediaInfo);
            let request = new chrome.cast.media.LoadRequest(mediaInfo);
            request.autoplay = true;
            request.currentTime = 0;
            console.debug('Created load request', request);
            console.log('Using session', this.session, this.session.loadMedia);
            let media = await new Promise((resolve, reject) => {
                this.session.getSessionObj().loadMedia(request, resolve, reject);
                console.log('load session running...');
            });
            console.log('got media', media);
        } catch (ex) {
            console.error('Failed to load media', url, ex);
            throw ex;
        } finally {
            console.log('finally clause');
        }
    }

    /**
     * Handle the user clicking on a video link.
     * @param {PointerEvent} event 
     */
    async handleLink(event) {
        const linkURL = new URL(event.target.href, location.href);

        if (!linkURL.pathname.startsWith('/v/')) {
            console.debug(`Ignoring unexpected link ${linkURL.pathname}`);
            return;
        }
        event.preventDefault();

        try {
            await this.loadMedia(linkURL.href);
            console.debug(`Played ${ linkURL.href }`);
        } catch (ex) {
            console.error(`Failed to load media ${ linkURL }:`, ex);
        }
    }

    /**
     * Event handler for cast.framework.CastContextEventType.CAST_STATE_CHANGED
     * @param {cast.framework.CastStateEventData} event 
     */
    onCastStateChanged(event) {
        console.log('cast-state-changed', event);
        if (event.castState !== cast.framework.CastState.CONNECTED) {
            // If the cast state is no longer connected, drop the things that
            // depend on it.
            this._player = undefined;
            this._controller = undefined;
        }
    }

    onMediaInfoChanged() {
        let info = this.session?.getMediaSession()?.media;
        console.log('media info changed:', info);
    }
}
