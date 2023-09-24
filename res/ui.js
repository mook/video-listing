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
