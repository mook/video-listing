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

class UIManager {
    /** @type UIManager? */
    static instance = undefined;
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
            server = `http://${ server }`
        }
        return new URL(server);
    }
    /**
     * @param {string} val The server name to set. 
     */
    set server(val) {
        window.localStorage.setItem("server", val);
    }

    async initialize() {
        if (!this.server) {
            document.documentElement.setAttribute("data-state", "login");
            return;
        }
        documentElement.setAttribute("data-state", "listing");
        await this.navigate("/");
    }

    /**
     * Handle the user logging in to a server
     */
    async login() {
        /** @type HTMLInputElement */
        const elem = document.getElementById("server");
        this.server = elem.value;
        if (this.server) {
            // Reinitialize to login
            await this.initialize();
        } else {
            document.getElementById("login-error").textContent = "Failed to parse server";
        }
    }

    /**
     * Join a path
     * @param {URL?} base The base URL
     * @param  {...string} inputs The parts to join; may start or end with slash
     * @returns URL The resulting URL
     */
    join(base, ...inputs) {
        const parts = inputs.flatMap(x => x.split('/')).filter().join('/');

        return new URL(parts, base ?? location.href);
    }

    /**
     * Navigate to the given path, getting a new listing
     * @param {string} urlPath The path to navigate to
     */
    async navigate(urlPath) {
        urlPath = urlPath.replace(/^\/+/g, '').replace(/\/+$/g, '');
        const listing = await this.getListing(urlPath);
        document.getElementById("heading").textContent = listing.name;
        document.getElementById("directories").replaceChildren(
            ...listing.directories.map(d => {
                const li = document.createElement('li');
                const a = document.createElement('a');

                li.appendChild(a);
                a.href = d.hash;
                a.textContent = d.name;

                return li;
            })
        );
        document.getElementById("files").replaceChildren(
            ...listing.files.map(f => {
                const li = document.createElement('li');
                const a = document.createElement('a');

                li.appendChild(a);
                a.href = this.join(null, urlPath, f.hash).href;
                a.setAttribute('onclick', 'CastManager.getInstance().handleLink(event)');

                return li;
            })
        )
    }

    /**
     * Get the listing for a given path
     * @param {string} urlPath The directory path to get
     * @returns {Promise<DirectoryListing>}
     */
    async getListing(urlPath) {
        const requestURL = this.join(this.server, 'j', urlPath);
        const resp = await fetch(requestURL);

        return await resp.json();
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
 * @property {ListingEntry[]} directories
 * @property {ListingEntry[]} files
 */

window.addEventListener("load", () => {
    UIManager
    .getInstance()
    .initialize()
    .then(() => console.log('UI initialized'))
    .catch(ex => console.log('error initializing UI', ex));
});
