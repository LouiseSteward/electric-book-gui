import {ControlTag} from './ControlTag';
import {Context} from './Context';
import {EBW} from './EBW';
import {PrintListener} from './PrintListener';
import {RepoFileEditorCM} from './RepoFileEditorCM2';
import {RepoEditorPage_NewFileDialog} from './RepoEditorPage_NewFileDialog2';
import {RepoEditorPage_RenameFileDialog} from './RepoEditorPage_RenameFileDialog2';

// import {FileContent,FileStat,FS} from './FS/FS';
// import {FSNotify} from './FS/FSNotify';
// import {FSOverlay} from './FS/FSOverlay';
// import {FSRemote} from './FS/FSRemote';
// import {FSReadCache} from './FS/FSReadCache';
// import {FSSession} from './FS/FSSession';

import {File} from './FS2/File';
import {FileState} from './FS2/FileState';
import {FS, FSStateAndPath} from './FS2/FS';
import {MemFS} from './FS2/MemFS';
import {WorkingDirFS} from './FS2/WorkingDirFS';
import {NotifyFS} from './FS2/NotifyFS';
import {ReadCacheFS} from './FS2/ReadCacheFS';
import {FileState} from './FS2/FileState';
import {Node,NodeType} from './Tree/Node';

// import {FSPrimeFromJS} from './FS/FSPrimeFromJS';

// import {FSFileList} from './FSFileList';
import {FileSystemConnector} from './FileSystemConnector';
import {SHA1} from './FS2/SHA1';

/**
 * RepoEditorPage is the JS controller for the page that allows
 * editing of a repo.
 *
 */
export class RepoEditorPage {
	protected editor: RepoFileEditorCM;
	protected FS: FSNotify;
	protected Root: Node;

	constructor(
		protected context: Context,
		filesListElement: HTMLElement,
		filesJson: any,
		protected proseIgnoreFunction: (name:string)=>boolean,
		filesAndHashes: Array<Array<string>>
	) {
		sessionStorage.clear();

		// This is my FileSystem stack, which will ensure that edits are stored in-browser,
		// and that we can cache reads from the WorkingDirectory.
		let repoKey = ":/" + context.Username + ":" + context.RepoOwner + ":" + context.RepoName + "/";
		let wdFS = new WorkingDirFS(this.context, null);
		let readCacheFS = new ReadCacheFS(SHA1(`cache` + repoKey), wdFS);
		let memFS = new MemFS(SHA1(`mem`+ repoKey), readCacheFS);
		this.FS = new NotifyFS(memFS);
		this.Root = new Node(null, ``, NodeType.DIR, null);

		this.editor = undefined;
		this.editor = new RepoFileEditorCM(
			context.RepoOwner, context.RepoName,
			document.getElementById('editor'), {
			Rename: ():void=>{
				return;
			}},
			this.FS
		);

		new FileSystemConnector(this.context,
			filesListElement,
			this.editor,
			this.FS,
			this.proseIgnoreFunction,
			filesJson,
			this.Root,
			filesAndHashes);

		new RepoEditorPage_NewFileDialog(
			this.context,
			document.getElementById('repo-new-file'),
			this.FS,
			this.editor);
		new RepoEditorPage_RenameFileDialog(
			this.context,
			document.getElementById(`editor-rename-button`),
			this.editor);

		new ControlTag(document.getElementById(`files-show-tag`),
			(showing:boolean)=>{

				// Set width of nav
				document.getElementById(`new-editor-files-nav`)
					.style.width = showing ? "20%":"0px";

				// Show/hide container (avoids leaving scrollbar visible)
				document.getElementById(`all-files-editor-container`).style.display = showing ? "block" : "none";

				// Hide repo actions
				document.getElementById(`repo-file-actions`)
					.style.visibility = showing ? `visible` : `hidden`;

				// Hide file-editor actions
				document.getElementById(`editor-actions`)
					.style.visibility = showing ? `visible` : `hidden`;

				// Hide filename
				document.querySelector(`.file-title`)
					.style.visibility = showing ? `visible` : `hidden`;

				// Hide footer
				document.getElementById(`page-footer`)
					.style.display = showing ? 'flex' : 'none';
			});


		document.getElementById(`repo-print-printer`).addEventListener('click', evt=>{
			evt.preventDefault(); evt.stopPropagation();
			EBW.Toast(`Creating your PDF. We'll open it in a new tab when it's ready.`);
			new PrintListener(this.context.RepoOwner, this.context.RepoName, `book`, `print`);
		});
		document.getElementById(`repo-print-screen`).addEventListener(`click`, evt=>{
			evt.preventDefault(); evt.stopPropagation();
			EBW.Toast(`Creating your PDF. We'll open it in a new tab when it's ready.`);
			new PrintListener(this.context.RepoOwner, this.context.RepoName, `book`, `screen`);
		});
		document.getElementById(`repo-jekyll`).addEventListener(`click`, evt=>{
			evt.preventDefault(); evt.stopPropagation();
			let l = document.location;
			let jekyllUrl = `${l.protocol}//${l.host}/jekyll-restart/` +
				`${this.context.RepoOwner}/${this.context.RepoName}/`;
			console.log(`URL = ${jekyllUrl}`);
			window.open(jekyllUrl, `${this.context.RepoOwner}-${this.context.RepoName}-jekyll`);
		});
		/**
		 * Catch any attempt to leave RepoEditorPage and
		 * check that the user has saved any changes.
		 */
		window.addEventListener(`beforeunload`, evt=>{
			evt.returnValue = `Any unsaved changes will be lost. Continue?`;
		});

		document.getElementById(`repo-save-all`).addEventListener(`click`, evt=>{
			evt.preventDefault(); evt.stopPropagation();
			let rs = document.getElementById(`repo-save-all`);
			rs.classList.add(`active`);
			this.SyncFiles().then(
				_=>rs.classList.remove(`active`)
			);
		});
	}

	AreFilesSynced() : Promise<boolean> {
		Promise.all(this.Root.files().map( (p:string)=>this.FS.FileStateAndPath(p) ))
		.then (
			(states:Array<FSStateAndPath>)=>{
				states = states.filter((fs)=>{
					return fs.ShouldSync();
				});
				return new Promise<boolean>(0==states.length);
			});
	}
	SyncFiles() : Promise {
		return Promise.all(this.Root.files().map( (p:string)=>this.FS.FileStateAndPath(p) ))
		.then (
			states=>
				Promise.all(
					states.filter( fs=>fs.ShouldSync() )
					.map( fs=>this.FS.Sync(fs.path) )
				)
		);
	}
}

window.RepoEditorPage = RepoEditorPage;
