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

// @ts-check

class UIManager {
    /** @type UIManager? */
    static instance = null;
    static getInstance() {
        UIManager.instance ??= new UIManager;
        return UIManager.instance;
    }

    /**
     * The stored server name.
     * @type URL | null
     */
    get server() {
        let server = window.localStorage.getItem("server");
        if (!server) {
            return null;
        }
        if (!server.includes("://")) {
            server = `http://${ server }`;
        }
        return new URL(server);
    }
    /**
     * @param {string} val The server name to set. 
     */
    set server(val) {
        window.localStorage.setItem("server", val);
    }

    /**
     * The server to use for videos.
     * @type URL | null
     */
    get videoServer() {
        let server = window.localStorage.getItem("video-server");
        if (!server) {
            return this.server;
        }
        if (!server.includes("://")) {
            server = `http://${ server }`;
        }
        return new URL(server) ?? this.server;
    }
    /** @param {string} val The server name to set. */
    set videoServer(val) {
        if (val) {
            window.localStorage.setItem("video-server", val);
        } else {
            window.localStorage.removeItem("video-server");
        }
    }

    /**
     * @typedef {"login"|"server"|"login-error"} ElementID_login
     * @typedef {"listing"|"heading"|"directories"|"files"} ElementID_listing
     * @typedef {"error"|"error-message"} ElementID_error
     * @typedef {ElementID_login|ElementID_listing|ElementID_error} ElementID
     */

    /**
     * Get an known element
     * @param {ElementID} elementId The id of the element to get
     * @returns {HTMLElement} The element
     */
    getElem(elementId) {
        return /** @type HTMLElement */(document.getElementById(elementId));
    }

    async initialize() {
        if (!this.server) {
            console.debug('No server found!');
            document.documentElement.setAttribute("data-state", "login");
            return;
        }
        console.debug('Using server:', this.server);
        document.documentElement.setAttribute("data-state", "listing");
        await this.navigate();
    }

    /**
     * Handle the user logging in to a server
     */
    async login() {
        const elem = /** @type {HTMLInputElement} */ (this.getElem("server"));
        this.server = elem.value;
        if (this.server) {
            // Reinitialize to login
            await this.initialize();
        } else {
            const elem = this.getElem("login-error");
            elem.textContent = "Failed to parse server";
        }
    }

    /**
     * Join a path
     * @param {URL?} base The base URL
     * @param  {...string} inputs The parts to join; may start or end with slash
     * @returns URL The resulting URL
     */
    join(base, ...inputs) {
        const parts = inputs.flatMap(x => x.split('/')).filter(x => x).join('/');

        return new URL(parts, base ?? location.href);
    }

    /**
     * Return the parts of the current URL
     * @returns {string[]} The parts of the current URL
     */
    get urlParts() {
        return location.hash.replace(/^#/, '').split('/').filter(x => x);
    }

    /**
     * Handle the user clicking on a navigation link.
     * @param {HTMLAnchorElement} elem The element being clicked.
     */
    handleNavigate(elem) {
        if (!elem.hasAttribute("data-path")) {
            console.error("Clicked on element with no destination:", elem);
            return;
        }
        location.hash = elem.getAttribute("data-path") ?? "";
        this.navigate().catch(ex => console.error('Error navigating:', ex, elem));
    }

    /**
     * Navigate to the path found in the URL.
     */
    async navigate() {
        const serverURL = /** @type {URL} */(this.server);
        const videoServerURL = this.videoServer ?? serverURL;
        const requestURL = new URL(`j/${this.urlParts.join('/')}`, serverURL);
        const resp = await fetch(requestURL);
        /** @type {DirectoryListing} */
        const listing = await resp.json();
        this.getElem("heading").textContent = listing.name;
        this.getElem("heading").setAttribute("data-path", this.urlParts.slice(0, -1).join("/"));
        document.title = listing.name;
        this.getElem("directories").replaceChildren(
            ...(listing.directories??[]).map(d => {
                const li = document.createElement('li');
                const a = document.createElement('a');
                const path = this.urlParts.concat(d.hash).join('/');

                li.appendChild(a);
                a.setAttribute('href', '#' + path);
                a.setAttribute('data-path', path);
                a.setAttribute("onclick", 'UIManager.getInstance().handleNavigate(this)');
                a.textContent = d.name;

                return li;
            })
        );
        this.getElem("files").replaceChildren(
            ...(listing.files??[]).map(f => {
                const li = document.createElement('li');
                const a = document.createElement('a');
                const path = this.urlParts.concat(f.hash).join('/');

                li.appendChild(a);
                a.setAttribute('href', new URL(`v/${ path }`, videoServerURL).href);
                a.setAttribute('onclick', 'CastManager.getInstance().handleLink(event)');
                a.textContent = f.name;

                return li;
            })
        )
    }
}

/**
 * @typedef {Object} ListingEntry
 * @property {string} name
 * @property {string} hash
 */

/**
 * DirectoryListing is the JS representation of `listing.directoryListing`.
 * @typedef {Object} DirectoryListing
 * @property {string} name
 * @property {string} path
 * @property {ListingEntry[]?} directories
 * @property {ListingEntry[]?} files
 */

window.addEventListener("load", () => {
    UIManager
    .getInstance()
    .initialize()
    .then(() => console.log('UI initialized'))
    .catch(ex => console.log('error initializing UI', ex));
});
