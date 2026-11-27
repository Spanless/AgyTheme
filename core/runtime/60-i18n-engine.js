  if (window.__ag_i18n_observer) {
    try {
      window.__ag_i18n_observer.disconnect();
    } catch (e) {}
    window.__ag_i18n_observer = null;
  }

  const DICT = Object.assign(Object.create(null), boot.i18nDict || {});
  window.__ag_i18n_dict = DICT;

  const DICT_LC = Object.create(null);
  for (const k in DICT) {
    if (k.indexOf(' ') === -1) continue;
    const lk = k.toLowerCase();
    if (lk in DICT_LC && DICT_LC[lk] !== DICT[k]) DICT_LC[lk] = null;
    else DICT_LC[lk] = DICT[k];
  }
  const lookupDict = (s) => {
    const v = DICT[s];
    if (typeof v === 'string') return v;
    if (s.indexOf(' ') === -1) return undefined;
    const lv = DICT_LC[s.toLowerCase()];
    return typeof lv === 'string' ? lv : undefined;
  };

  let scanBodyFn = null;
  const I18N_OBS_OPTS = {
    childList: true,
    subtree: true,
    characterData: true,
    attributes: true,
    attributeFilter: ['title', 'aria-label', 'placeholder']
  };

  if (MODULES.i18n) {
    function splitTopLevelSelectors(selStr) {
      const parts = [];
      let cur = '';
      let bracketDepth = 0;
      let parenDepth = 0;
      let quote = '';
      const s = String(selStr || '');
      for (let i = 0; i < s.length; i++) {
        const ch = s[i];
        if (quote) {
          cur += ch;
          if (ch === quote && s[i - 1] !== '\\') quote = '';
          continue;
        }
        if (ch === '"' || ch === "'") {
          quote = ch;
          cur += ch;
        } else if (ch === '[') {
          bracketDepth++;
          cur += ch;
        } else if (ch === ']') {
          if (bracketDepth > 0) bracketDepth--;
          cur += ch;
        } else if (ch === '(') {
          parenDepth++;
          cur += ch;
        } else if (ch === ')') {
          if (parenDepth > 0) parenDepth--;
          cur += ch;
        } else if (ch === ',' && bracketDepth === 0 && parenDepth === 0) {
          if (cur.trim()) parts.push(cur.trim());
          cur = '';
        } else {
          cur += ch;
        }
      }
      if (cur.trim()) parts.push(cur.trim());
      return parts;
    }

    const CONTAINER_IMMUTABLE_EXCLUDE_SELECTOR =
      'code, pre, kbd, samp, style, script, noscript, [data-markdown-root], [data-testid="user-message-content"], .agy-user-bubble, .monaco-editor, .monaco-diff-editor, .xterm, #ag-live-metrics-pill, .ag-live-metrics-pill, #ag-live-metrics-tooltip, #ag-turn-metrics-tooltip, .ag-turn-metrics, #ag-telemetry-dock-btn, #ag-telemetry-dock, #ag-telemetry-sash, #ag-telemetry-hover';
    const TEXT_IMMUTABLE_EXCLUDE_SELECTOR = 'textarea, input, ' + CONTAINER_IMMUTABLE_EXCLUDE_SELECTOR;

    const mergedExcludeParts = splitTopLevelSelectors(DEFAULT_EXCLUDE_I18N);
    const userExcludeParts = splitTopLevelSelectors(SELECTORS.excludeI18n);
    for (let i = 0; i < userExcludeParts.length; i++) {
      if (!mergedExcludeParts.includes(userExcludeParts[i])) {
        mergedExcludeParts.push(userExcludeParts[i]);
      }
    }
    const EXCLUDE_SELECTOR = mergedExcludeParts.join(', ');
    const FORM_ATTR_EXCLUDE_SELECTOR = mergedExcludeParts
      .filter((tok) => {
        const low = tok.toLowerCase();
        return low !== 'input' && low !== 'textarea';
      })
      .join(', ');

    const immutableTextExclCache = new WeakMap();
    const immutableContainerExclCache = new WeakMap();

    const RE_HAS_CJK = /[\u4e00-\u9fa5]/;
    const RE_PURE_ALPHA = /^\s*[a-zA-Z]+\s*$/;
    const RE_HAS_ALPHA = /[a-zA-Z]/;
    const RE_REL_TIME = /^(\d+)\s*(s|sec|secs|m|min|mins|h|hr|hrs|d|w|mo|y|yr|yrs)\s+ago$/i;
    const REL_UNIT_MAP = {
      s: '秒',
      sec: '秒',
      secs: '秒',
      m: '分钟',
      min: '分钟',
      mins: '分钟',
      h: '小时',
      hr: '小时',
      hrs: '小时',
      d: '天',
      w: '周',
      mo: '个月',
      y: '年',
      yr: '年',
      yrs: '年'
    };
    const UNIT_MAP_SHORT = { s: '秒', m: '分钟', h: '小时', d: '天', w: '周', mo: '个月', y: '年', yr: '年' };
    const NAMED_PERIOD_MAP = { weekly: '每周', daily: '每日', monthly: '每月' };
    const MONTH_SHORT_MAP = {
      jan: 1,
      feb: 2,
      mar: 3,
      apr: 4,
      may: 5,
      jun: 6,
      jul: 7,
      aug: 8,
      sep: 9,
      oct: 10,
      nov: 11,
      dec: 12
    };
    // Antigravity onBlur checks relatedTarget.getAttribute("aria-label") === "Cancel".
    const SKIP_ARIA_LABEL_VALUES = new Set(['Cancel']);

    const RE_PCT_LEFT = /^(\d+(?:\.\d+)?)%\s*left$/i;
    const RE_WINDOW_LIMIT_COMBINED = /^(\d+)\s*([smhd])\s+limit,\s*resets in\s+(.+)$/i;
    const RE_WINDOW_LIMIT = /^(\d+)\s*([smhd])\s+limit$/i;
    const RE_NAMED_LIMIT_RESETS = /^(Weekly|Daily|Monthly)\s+limit,\s*resets in\s+(.+)$/i;
    const RE_MODEL_NAMED_LIMIT = /^(.+?)\s+(Weekly|Daily|Monthly)\s+limit$/i;
    const RE_RESETS_IN = /^Resets in\s+(.+)$/i;
    const RE_SEARCH_GOOGLE = /^Search Google for "(.+)"$/;
    const RE_UPDATED_AT =
      /^Updated\s+(?:((?:Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)\s+\d{1,2}|\d{1,2}月\d{1,2}日),\s*)?((?:上午|下午)?\s*\d{1,2}:\d{2}(?:\s*[AP]M)?)$/;

    const RE_FILES_CHANGED = /^(\d+)\s+files?\s+changed$/i;
    const RE_TOOLS = /^(\d+)\s+tools?$/i;
    const RE_DEMOTED = /^(\d+)\s+demoted$/i;
    const RE_EXCLUDED = /^(\d+)\s+excluded$/i;
    const RE_TOOLS_EXCLUDED = /^(\d+)\s+tools?\s+excluded$/i;
    const RE_ACTIVE_CONVS = /^(\d+)\s+active\s+conversations$/i;
    const RE_UPDATES_COUNT = /^(\d+)\s+updates?$/i;
    const RE_PROJECTS_COUNT = /^(\d+)\s+projects?$/i;
    const RE_CALLS_MB = /^(\d+)\s+calls?\s*·\s*([\d.]+\s*MB)$/i;
    const RE_WAITING_ON_YOU = /^(\d+)\s+waiting\s+on\s+you$/i;
    const RE_HIDDEN_HOSTS = /^(\d+)\s+hidden\s+hosts?$/i;
    const RE_COMMENTS_COUNT = /^(\d+)\s+comments?$/i;
    const RE_DRAFTS_COUNT = /^(\d+)\s+drafts?$/i;
    const RE_LINES_ABOVE_HIDDEN = /^(\d+)\s+lines?\s+above\s+hidden$/i;
    const RE_SEE_ALL = /^See all\s*\((\d+)\)$/i;
    const RE_SEND_ALL = /^Send all\s*\((\d+)\)$/i;
    const RE_SHOW_BREAKDOWNS = /^Show\s+(\d+)\s+breakdowns?$/i;
    const RE_SHOW_MORE_SUBAGENTS = /^Show\s+(\d+)\s+more\s+subagents?$/i;
    const RE_SHOW_EARLIER_STEPS = /^Show\s+(\d+)\s+earlier\s+steps?$/i;
    const RE_SHOW_SUBMITTED = /^Show submitted\s*\((\d+)\)$/i;
    const RE_SHOW_MORE_PAREN = /^Show more\s*\((\d+)\)$/i;
    const RE_START_BEST_OF = /^Start Best-of-(\d+)$/i;
    const RE_STOP_N_SELECTED = /^Stop\s+(\d+)\s+selected$/i;
    const RE_GENERATION_STEPS = /^Generation Steps\s*\((\d+)\)$/i;
    const RE_EXPORTING_PCT = /^Exporting\s+(\d+)%$/i;
    const RE_PREVIEW_UNAVAILABLE_REASON = /^Preview unavailable\s*[—–-]\s*(.+)$/;
    const RE_QUESTION_OF = /^Question\s+(\d+)\s+of\s+(\d+)$/i;
    const RE_LOW_CONTRAST = /^Low contrast ratio\s*\(([\d.]+:1)\)\.\s*Text may be hard to read\.$/i;
    const RE_RUNS_ON = /^Runs on\s+(.+)$/;
    const RE_SIGNING_IN_ON = /^You're signing in on\s+(.+?)\.\s*Your sign-in stays on that machine\.$/;
    const RE_ZIPPING_FOLDER = /^Zipping\s+(.+?)(?:…|\.\.\.)$/i;
    const RE_PERM_DELETE = /^Permanently delete\s+(.*)\s+including\s+(\d+)\s+active\s+conversations\.?$/i;
    const RE_WORKED_FOR_UPDATES = /^Worked for\s+([<0-9smhd\s]+?)\s*·\s*(\d+)\s+updates?$/i;
    const RE_WORKED_FOR = /^Worked for\s+([<0-9smhd]+(?:\s+\d+[smhd])*)$/i;
    const RE_THOUGHT_FOR = /^Thought for\s+([<0-9smhd]+(?:\s+\d+[smhd])*)$/i;
    const RE_RUNNING_FOR = /^Running\s+for\s+([<0-9smhd]+(?:\s+\d+[smhd])*)$/i;
    const RE_EPHEMERAL_SUMMARY =
      /^(Explored(?:\s+(?:\d+\s+)?(?:files?|artifacts?)(?:\s+and\s+(?:\d+\s+)?(?:files?|artifacts?))?)?|Ran\s+(?:\d+\s+)?commands?|Edited(?:\s+(?:\d+\s+)?(?:files?|artifacts?)(?:\s+and\s+(?:\d+\s+)?(?:files?|artifacts?))?)?)(?:,\s*(explored(?:\s+(?:\d+\s+)?(?:files?|artifacts?)(?:\s+and\s+(?:\d+\s+)?(?:files?|artifacts?))?)?|ran\s+(?:\d+\s+)?commands?|edited(?:\s+(?:\d+\s+)?(?:files?|artifacts?)(?:\s+and\s+(?:\d+\s+)?(?:files?|artifacts?))?)?))*$/i;
    const EPHEMERAL_CLAUSE_MAP = {
      explored: '已探索',
      'explored file': '已探索文件',
      'explored files': '已探索文件',
      'explored artifact': '已探索交付工件',
      'explored artifacts': '已探索交付工件',
      'explored file and artifact': '已探索文件与交付工件',
      'explored files and artifact': '已探索文件与交付工件',
      'explored file and artifacts': '已探索文件与交付工件',
      'explored files and artifacts': '已探索文件与交付工件',
      'ran command': '已运行命令',
      'ran commands': '已运行命令',
      edited: '已编辑',
      'edited file': '已编辑文件',
      'edited files': '已编辑文件',
      'edited artifact': '已编辑交付工件',
      'edited artifacts': '已编辑交付工件',
      'edited file and artifact': '已编辑文件与交付工件',
      'edited files and artifact': '已编辑文件与交付工件',
      'edited file and artifacts': '已编辑文件与交付工件',
      'edited files and artifacts': '已编辑文件与交付工件'
    };
    const RE_EPHEMERAL_COUNT_CLAUSE = /^(explored|edited|ran)\s+(\d+)\s+(files?|artifacts?|commands?)$/i;
    function translateEphemeralClause(clause) {
      const low = clause.trim().toLowerCase();
      if (EPHEMERAL_CLAUSE_MAP[low]) return EPHEMERAL_CLAUSE_MAP[low];
      const mCount = low.match(RE_EPHEMERAL_COUNT_CLAUSE);
      if (mCount) {
        const vMap = { explored: '已探索', edited: '已编辑', ran: '已运行' };
        const nMap = {
          file: '个文件',
          files: '个文件',
          artifact: '个交付工件',
          artifacts: '个交付工件',
          command: '条命令',
          commands: '条命令'
        };
        return vMap[mCount[1]] + ' ' + mCount[2] + ' ' + nMap[mCount[3]];
      }
      return clause.trim();
    }
    const RE_EPHEMERAL_ACTIVE = /^(Editing|Reading|Listing|Searching)\s+(.+)$/;
    const EPHEMERAL_VERB_MAP = {
      Editing: '正在编辑',
      Reading: '正在读取',
      Listing: '正在列出',
      Searching: '正在搜索'
    };
    const EPHEMERAL_NOUN_MAP = {
      file: '文件',
      files: '文件',
      artifact: '交付工件',
      artifacts: '交付工件',
      'file and artifact': '文件与交付工件',
      'files and artifact': '文件与交付工件',
      'file and artifacts': '文件与交付工件',
      'files and artifacts': '文件与交付工件'
    };
    const RE_RUNNING_CMD = /^Running\s+(?!commands?\b|processes\b|for\b|\(Dev\b)(.+)$/;
    const RE_DELETED_RECORDINGS = /^Deleted\s+(\d+)\s+recordings?\s*\(([\d.]+\s*MB)\)$/i;
    const RE_DELETE_RECORDINGS_CONFIRM =
      /^This deletes\s+(\d+)\s+call recordings?\s*\(([\d.]+\s*MB)\)\s+from your machine\.\s*You can't undo this\.$/i;
    const RE_USED_LIMIT = /^You have used (some|all|most) of your (weekly|5-hour|five-hour|daily) limit,?\s*it will fully refresh in (.*)\.?$/i;
    const RE_NOT_USED_LIMIT = /^You have not used any of your (weekly|5-hour|five-hour|daily) limit\.?$/i;
    const RE_ARIA_BRANCH = /^Branch, current:\s*(.*)$/i;
    const RE_ARIA_SHORTCUT = /^(Remove shortcut|Shortcut)\s+(.+?)\s+for\s+(.+)$/;
    const RE_ARIA_ADD_SHORTCUT = /^Add shortcut for\s+(.+)$/;
    const RE_ARIA_CRUMB = /^(Project|Conversation):\s+(.+)$/;
    const RE_ARIA_CLOSE_NAMED = /^Close\s+(\S+)$/;
    const RE_ARIA_LINES_DIFF = /^(\d+)\s+lines?\s+added,\s*(\d+)\s+lines?\s+removed$/i;
    const RE_TITLE_RULES_TOKENS = /^Rules:\s*([\d,]+)\s+tokens$/;
    const RE_SEND_FEEDBACK_AS = /^Send feedback as\s+(.+)$/;
    const RE_GREETING_NAMED = /^([^,]+),\s+([^,.?]+?)([.,])\s+(.+\?)$/;
    const RE_SHOW_N_MORE = /^Show\s+(\d+)\s+more(?:\.\.\.|…)?$/i;
    const RE_ARIA_PROJECT = /^Select project(?:, current)?:\s*(.*)$/i;
    const RE_ARIA_LOCATION = /^(?:Where the agent runs, current|Select location(?:, current)?):\s*(.*)$/i;
    const RE_ARIA_VERSION = /^Version\s+([\d.]+(?:\s*·\s*\S+)?)$/i;
    const RE_ARIA_MODEL = /^(?:Select model|Model(?:\s+selector)?)(?:,\s*current)?:\s*(.*)$/i;
    const RE_ARIA_PERM_SEL =
      /^(?:(?:Permission mode|Agent permissions preset)\s+selector(?:,\s*current)?|(?:Permission mode|Agent permissions preset),\s*current):\s*(.*)$/i;
    const RE_ARIA_PERM_FIXED = /^(?:Permission mode|Agent permissions preset):\s*([^,(]+?)(?:\s*(?:,\s*(.+?)|\(([^)]+)\)))?$/i;
    const RE_ARIA_TURN = /^(?:Conversation turn\s+(\d+)\s+by\s+(User|Agent|Assistant)|Turn\s+(\d+)\s+of\s+(\d+))$/i;
    const RE_ARIA_LOAD_OLDER = /^(?:Load\s+(\d+)\s+older\s+turns?.*|Load older messages,\s*showing\s+(\d+)\s+of\s+(\d+))$/i;
    const RE_YOUR_PLAN = /^Your Plan:\s*([^\r\n]{1,80})$/;
    const RE_AVAILABLE_AI_CREDITS = /^Available AI Credits:\s*([^\r\n]{1,40})$/i;
    const RE_EXITED_CODE_ONLY = /^Exited with code\s+(-?\d+)(\.)?$/i;
    const RE_SUMMARY_EXITED_CODE = /^([^\r\n]{1,220}?)\s*·\s*Exited with code\s+(-?\d+)$/;
    const RE_STARTED_SIDE_QUOTE = /^Started\s+(from|during)\s+[“"](.+)[”"]$/;
    const RE_ARIA_PLAYBACK_SPEED = /^Playback speed\s+([\d.]+x)$/i;
    const RE_ARIA_ZOOM_RESET = /^Zoom\s+(\d+)%,\s*reset to fit$/i;
    const RE_ARIA_PLAN_STEPS = /^Plan,\s*(\d+)\s+of\s+(\d+)\s+steps?\s+done$/i;
    const RE_ARIA_GO_TO_TURN = /^Go to turn\s+(\d+)\s+of\s+(\d+)$/i;
    const RE_ARIA_OPEN_SUBAGENT = /^Open Subagent:\s*(.+)$/i;
    const RE_ARIA_VIEW_DIFF_FOR = /^View Diff for\s+(.+)$/i;
    const RE_ARIA_DELETE_COOKIES_FOR = /^Delete cookies for\s+(.+)$/i;
    const RE_ARIA_REVOKE = /^Revoke(?: (all))?:\s*(.+)$/i;
    const RE_ARIA_MORE_ACTIONS_FOR = /^More (actions|options) for\s+(.+)$/i;
    const RE_ARIA_RESET_DEFAULT = /^Reset\s+(.+?)\s+to default$/i;
    const RE_ARIA_RECORDING_SHORTCUT =
      /^Recording shortcut for\s+(.+?)\.\s*Press keys to bind,\s*Enter to save,\s*Escape to cancel\.$/;
    const RE_ARIA_OPEN_URL_SYS = /^Open URL in system browser:\s*(.+)$/i;
    const RE_ARIA_GO_TO_PAGE = /^Go to (page|slide)\s+(\d+)$/i;
    const RE_ARIA_DELETE_SIDE_Q = /^Delete side question:\s*(.+)$/i;
    const RE_ARIA_SWITCH_ENV = /^Switch environment\s*\((.+)\)$/i;
    const RE_ARIA_DETAILS_FOR = /^Details for\s+(.+)$/i;
    const RE_ARIA_SHOW_IN_FOLDER = /^Show\s+(.+?)\s+in folder$/i;
    const RE_ARIA_DOWNLOAD_PROGRESS = /^(.+?)\s+download progress$/i;
    const RE_ARIA_REMOVE_FROM_LIST = /^Remove\s+(.+?)\s+from list$/i;
    const RE_ARIA_GO_TO_BOARD_SEQ = /^Go to #(\d+) on the Board$/i;

    function formatCompactDurationZh(str) {
      const s = String(str || '').trim();
      if (s === '<1m' || s === '< 1m') return '不到 1 分钟';
      return s
        .replace(/(\d+)\s*d\b/gi, '$1 天')
        .replace(/(\d+)\s*h\b/gi, '$1 小时')
        .replace(/(\d+)\s*m\b/gi, '$1 分钟')
        .replace(/(\d+)\s*s\b/gi, '$1 秒')
        .replace(/\s+/g, ' ')
        .trim();
    }

    function formatUpdatedAtZh(datePart, timePart) {
      if (!datePart) return '更新于 ' + timePart.trim();
      const mEnDate = datePart.trim().match(/^([A-Za-z]{3})\s+(\d{1,2})$/);
      if (mEnDate) {
        const mNum = MONTH_SHORT_MAP[mEnDate[1].toLowerCase()];
        if (mNum) {
          return '更新于 ' + mNum + '月' + mEnDate[2] + '日 ' + timePart.trim();
        }
      }
      return '更新于 ' + datePart.trim() + ' ' + timePart.trim();
    }

    function isExcludedElement(el, batchSafeSet) {
      if (!el || el.nodeType !== 1) return true;
      if (immutableTextExclCache.get(el) === true) return true;
      if (batchSafeSet && batchSafeSet.has(el)) return false;
      if (el.closest(TEXT_IMMUTABLE_EXCLUDE_SELECTOR)) {
        if (el.isConnected) immutableTextExclCache.set(el, true);
        return true;
      }
      if (el.closest(EXCLUDE_SELECTOR)) {
        return true;
      }
      if (batchSafeSet) batchSafeSet.add(el);
      return false;
    }

    function canTranslateElementAttrs(el, batchSafeSet) {
      if (!el || el.nodeType !== 1) return false;
      if (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA') {
        if (immutableContainerExclCache.get(el) === true) return false;
        if (el.closest(CONTAINER_IMMUTABLE_EXCLUDE_SELECTOR)) {
          if (el.isConnected) immutableContainerExclCache.set(el, true);
          return false;
        }
        return FORM_ATTR_EXCLUDE_SELECTOR ? !el.closest(FORM_ATTR_EXCLUDE_SELECTOR) : true;
      }
      return !isExcludedElement(el, batchSafeSet);
    }

    function isInHeaderOrBreadcrumb(el) {
      return !!(el && el.closest && el.closest('header, [data-testid="breadcrumb-segment"]'));
    }

    function resolveContextualDictTranslation(trimmed, contextEl, baseVal) {
      if (!contextEl || !contextEl.closest) return baseVal;
      if (trimmed === 'Plan' && baseVal === '计划' && contextEl.closest('[role="dialog"]')) {
        return '方案套餐';
      }
      if (trimmed === 'Default') {
        if (contextEl.closest('[data-testid="permissions-default-tag"]')) {
          return '全局默认';
        }
        if (contextEl.closest('[data-testid="permissions-preset-option-default"], [data-testid="permissions-selector-trigger"]')) {
          return '敏感确认 (Default)';
        }
        const permOpt = contextEl.closest('[role="option"], [role="menuitem"]');
        const optTxt = permOpt ? permOpt.textContent || '' : '';
        if (optTxt.includes('Ask before sensitive operations') || optTxt.includes('执行敏感操作前进行确认')) {
          return '敏感确认 (Default)';
        }
      }
      if (
        trimmed === 'View' &&
        baseVal === '视图' &&
        contextEl.closest('button') &&
        !contextEl.closest('[aria-haspopup], [role="menuitem"], [role="menu"], header')
      ) {
        return '查看';
      }
      if (trimmed === 'New' && baseVal === '新建') {
        if (contextEl.closest('[data-testid="model-selector-panel"], [data-testid="model-selector-item"]')) return '新';
        if (!contextEl.closest('button, [role="button"], [role="menuitem"], a')) return '新';
      }
      if (trimmed === 'Type' && baseVal === '类型') {
        const pTxt = contextEl.parentElement ? contextEl.parentElement.textContent || '' : '';
        if (/\band select\b|\/plan\b/i.test(pTxt)) return '键入';
      }
      if (trimmed === 'Actions' && baseVal === '操作') {
        const menuContainer = contextEl.closest('[role="menu"], [data-radix-popper-content-wrapper]');
        if (menuContainer && !contextEl.closest('[role="dialog"], [cmdk-list]')) {
          const mTxt = menuContainer.textContent || '';
          if (mTxt.includes('Mentions') || mTxt.includes('提及引用') || mTxt.includes('Add Context') || mTxt.includes('添加上下文')) {
            return '快捷指令 (/)';
          }
        }
      }
      if (trimmed === 'Machine' && baseVal === '整机') {
        const rowContainer = contextEl.closest('div') && contextEl.closest('div').parentElement;
        if (rowContainer && rowContainer.querySelector('[data-testid="new-sidecar-cloud-environment"]')) {
          return '运行主机';
        }
      }
      if (trimmed === 'Blocked' && baseVal === '已阻塞' && !contextEl.closest('[data-testid="conversation-row-sidebar"]')) {
        const aria = contextEl.getAttribute ? contextEl.getAttribute('aria-label') || '' : '';
        if (aria.startsWith('Blocked') || contextEl.closest('[class*="destructive"]')) {
          return '已受限';
        }
      }
      if (trimmed === 'when working') {
        const pTxt = contextEl.parentElement ? contextEl.parentElement.textContent || '' : '';
        if (pTxt.includes('Global Permissions') || pTxt.includes('全局权限')) return '，适用于';
      }
      if ((trimmed === 'in this project' || trimmed === 'in this workspace') && baseVal === undefined) {
        const pTxt = contextEl.parentElement ? contextEl.parentElement.textContent || '' : '';
        if (pTxt.includes('Global Permissions') || pTxt.includes('全局权限') || pTxt.includes('when working') || pTxt.includes('适用于')) {
          return trimmed === 'in this project' ? '当前项目' : '当前工作区';
        }
      }
      return baseVal;
    }

    function computeTextTranslation(raw, contextEl) {
      if (!raw) return null;
      const trimmed = raw.trim();
      if (!trimmed) return null;

      let replacement = resolveContextualDictTranslation(trimmed, contextEl, lookupDict(trimmed));
      if (typeof replacement !== 'string' && trimmed.endsWith('.')) {
        const withoutDot = lookupDict(trimmed.slice(0, -1));
        if (typeof withoutDot === 'string') {
          replacement = withoutDot.endsWith('。') ? withoutDot : withoutDot + '。';
        }
      }
      if (typeof replacement === 'string') {
        if (RE_HAS_CJK.test(replacement) && RE_PURE_ALPHA.test(raw)) {
          return replacement;
        }
        return raw.replace(trimmed, () => replacement);
      }

      if (!RE_HAS_ALPHA.test(trimmed)) return null;

      if (trimmed.endsWith('?')) {
        const mGreet = trimmed.match(RE_GREETING_NAMED);
        const greetBase = mGreet && lookupDict(mGreet[1] + mGreet[3] + ' ' + mGreet[4]);
        if (greetBase) {
          const cut = greetBase.indexOf('，');
          const zh = cut > 0 ? greetBase.slice(0, cut) + '，' + mGreet[2] + '，' + greetBase.slice(cut + 1) : greetBase;
          return raw.replace(trimmed, () => zh);
        }
      }

      const c0 = trimmed.charCodeAt(0);
      if (c0 >= 48 && c0 <= 57) {
        if (RE_PCT_LEFT.test(trimmed)) return raw.replace(RE_PCT_LEFT, '$1% 剩余');
        const mWinComb = trimmed.match(RE_WINDOW_LIMIT_COMBINED);
        if (mWinComb) {
          const u = UNIT_MAP_SHORT[mWinComb[2].toLowerCase()] || mWinComb[2];
          return raw.replace(trimmed, () => mWinComb[1] + ' ' + u + '限额，' + formatCompactDurationZh(mWinComb[3]) + '后重置');
        }
        const mWin = trimmed.match(RE_WINDOW_LIMIT);
        if (mWin) {
          const u = UNIT_MAP_SHORT[mWin[2].toLowerCase()] || mWin[2];
          return raw.replace(trimmed, () => mWin[1] + ' ' + u + '限额');
        }
        const mRel = trimmed.match(RE_REL_TIME);
        if (mRel) {
          const unit = REL_UNIT_MAP[mRel[2].toLowerCase()] || mRel[2];
          return raw.replace(trimmed, () => mRel[1] + ' ' + unit + '前');
        }
        if (RE_UPDATES_COUNT.test(trimmed)) return raw.replace(RE_UPDATES_COUNT, '$1 条进展');
        if (RE_FILES_CHANGED.test(trimmed)) return raw.replace(RE_FILES_CHANGED, '$1 个文件变更');
        if (RE_TOOLS.test(trimmed)) return raw.replace(RE_TOOLS, '$1 个工具');
        if (RE_DEMOTED.test(trimmed)) return raw.replace(RE_DEMOTED, '$1 项已降级');
        if (RE_EXCLUDED.test(trimmed)) return raw.replace(RE_EXCLUDED, '$1 项已排除');
        if (RE_TOOLS_EXCLUDED.test(trimmed)) return raw.replace(RE_TOOLS_EXCLUDED, '$1 个工具已排除');
        if (RE_ACTIVE_CONVS.test(trimmed)) return raw.replace(RE_ACTIVE_CONVS, '$1 个进行中的会话');
        if (RE_PROJECTS_COUNT.test(trimmed)) return raw.replace(RE_PROJECTS_COUNT, '$1 个项目');
        if (RE_CALLS_MB.test(trimmed)) return raw.replace(RE_CALLS_MB, '$1 次通话 · $2');
        if (RE_WAITING_ON_YOU.test(trimmed)) return raw.replace(RE_WAITING_ON_YOU, '$1 项等待你处理');
        if (RE_HIDDEN_HOSTS.test(trimmed)) return raw.replace(RE_HIDDEN_HOSTS, '$1 个隐藏主机');
        if (RE_COMMENTS_COUNT.test(trimmed)) return raw.replace(RE_COMMENTS_COUNT, '$1 条评论');
        if (RE_DRAFTS_COUNT.test(trimmed)) return raw.replace(RE_DRAFTS_COUNT, '$1 份草稿');
        if (RE_LINES_ABOVE_HIDDEN.test(trimmed)) return raw.replace(RE_LINES_ABOVE_HIDDEN, '上方已隐藏 $1 行');
        return null;
      }

      const firstUpper = trimmed[0].toUpperCase();
      if (firstUpper === 'S') {
        if (RE_SEE_ALL.test(trimmed)) return raw.replace(RE_SEE_ALL, '查看全部 ($1)');
        if (RE_SEND_ALL.test(trimmed)) return raw.replace(RE_SEND_ALL, '全部发送 ($1)');
        if (RE_SHOW_BREAKDOWNS.test(trimmed)) return raw.replace(RE_SHOW_BREAKDOWNS, '展开明细 ($1 项)');
        if (RE_SHOW_MORE_SUBAGENTS.test(trimmed)) return raw.replace(RE_SHOW_MORE_SUBAGENTS, '显示另外 $1 个子代理');
        if (RE_SHOW_EARLIER_STEPS.test(trimmed)) return raw.replace(RE_SHOW_EARLIER_STEPS, '展开 $1 个更早步骤');
        if (RE_SHOW_SUBMITTED.test(trimmed)) return raw.replace(RE_SHOW_SUBMITTED, '显示已提交 ($1)');
        if (RE_SHOW_MORE_PAREN.test(trimmed)) return raw.replace(RE_SHOW_MORE_PAREN, '显示更多 ($1)');
        if (RE_SHOW_N_MORE.test(trimmed)) return raw.replace(RE_SHOW_N_MORE, '再显示 $1 项...');
        if (RE_START_BEST_OF.test(trimmed)) return raw.replace(RE_START_BEST_OF, '开始 $1 选 1 (Best-of-$1)');
        if (RE_STOP_N_SELECTED.test(trimmed)) return raw.replace(RE_STOP_N_SELECTED, '停止所选 $1 项');
        const mFeedbackAs = trimmed.match(RE_SEND_FEEDBACK_AS);
        if (mFeedbackAs) return raw.replace(trimmed, () => '以 ' + mFeedbackAs[1] + ' 的身份发送反馈');
        if (RE_SEARCH_GOOGLE.test(trimmed)) return raw.replace(RE_SEARCH_GOOGLE, (_, q) => '使用 Google 搜索“' + q + '”');
        if (!isInHeaderOrBreadcrumb(contextEl)) {
          const mSearch = trimmed.match(RE_EPHEMERAL_ACTIVE);
          if (mSearch) {
            const noun = EPHEMERAL_NOUN_MAP[mSearch[2].toLowerCase()];
            const out = noun ? EPHEMERAL_VERB_MAP[mSearch[1]] + noun : EPHEMERAL_VERB_MAP[mSearch[1]] + ' ' + mSearch[2];
            return raw.replace(trimmed, () => out);
          }
        }
      } else if (firstUpper === 'P') {
        if (RE_PERM_DELETE.test(trimmed)) {
          return raw.replace(RE_PERM_DELETE, (_, target, cnt) => '永久删除 ' + target + ' 及 ' + cnt + ' 个进行中的会话。');
        }
        const mPrevUnavail = trimmed.match(RE_PREVIEW_UNAVAILABLE_REASON);
        if (mPrevUnavail) {
          return raw.replace(trimmed, () => '无法预览 — ' + mPrevUnavail[1].trim());
        }
      } else if (firstUpper === 'G') {
        if (RE_GENERATION_STEPS.test(trimmed)) return raw.replace(RE_GENERATION_STEPS, '生成步骤 ($1)');
      } else if (firstUpper === 'Q') {
        if (RE_QUESTION_OF.test(trimmed)) return raw.replace(RE_QUESTION_OF, '第 $1 / $2 个问题');
      } else if (firstUpper === 'W' || firstUpper === 'M') {
        const mNamed = trimmed.match(RE_NAMED_LIMIT_RESETS);
        if (mNamed) {
          const pZh = NAMED_PERIOD_MAP[mNamed[1].toLowerCase()] || mNamed[1];
          return raw.replace(trimmed, () => pZh + '限额，' + formatCompactDurationZh(mNamed[2]) + '后重置');
        }
        if (firstUpper === 'W') {
          if (RE_WORKED_FOR_UPDATES.test(trimmed)) {
            return raw.replace(RE_WORKED_FOR_UPDATES, (_, dur, cnt) => '已工作 ' + dur.trim() + ' · ' + cnt + ' 条进展');
          }
          if (RE_WORKED_FOR.test(trimmed)) return raw.replace(RE_WORKED_FOR, '已工作 $1');
        }
      } else if (firstUpper === 'T') {
        if (RE_THOUGHT_FOR.test(trimmed)) return raw.replace(RE_THOUGHT_FOR, '已思考 $1');
        if (RE_DELETE_RECORDINGS_CONFIRM.test(trimmed)) {
          return raw.replace(
            RE_DELETE_RECORDINGS_CONFIRM,
            '这将从您的设备中删除 $1 条通话录音（$2），此操作无法撤销。'
          );
        }
      } else if (firstUpper === 'E' || firstUpper === 'R' || firstUpper === 'L') {
        if (firstUpper === 'E' && RE_EXPORTING_PCT.test(trimmed)) {
          return raw.replace(RE_EXPORTING_PCT, '正在导出 $1%');
        }
        if (firstUpper === 'L' && RE_LOW_CONTRAST.test(trimmed)) {
          return raw.replace(RE_LOW_CONTRAST, '对比度较低（$1），文字可能难以阅读。');
        }
        if (firstUpper === 'R') {
          const mResets = trimmed.match(RE_RESETS_IN);
          if (mResets) {
            return raw.replace(trimmed, () => formatCompactDurationZh(mResets[1]) + '后重置');
          }
          const mRunsOn = trimmed.match(RE_RUNS_ON);
          if (mRunsOn) {
            return raw.replace(trimmed, () => '运行于 ' + mRunsOn[1].trim());
          }
        }
        if (RE_EPHEMERAL_SUMMARY.test(trimmed)) {
          const translatedSummary = trimmed
            .split(/,\s*/)
            .map((part) => translateEphemeralClause(part))
            .join('、');
          return raw.replace(trimmed, () => translatedSummary);
        }
        const inHdr = isInHeaderOrBreadcrumb(contextEl);
        if (!inHdr) {
          const mActive = trimmed.match(RE_EPHEMERAL_ACTIVE);
          if (mActive) {
            const noun = EPHEMERAL_NOUN_MAP[mActive[2].toLowerCase()];
            const out = noun ? EPHEMERAL_VERB_MAP[mActive[1]] + noun : EPHEMERAL_VERB_MAP[mActive[1]] + ' ' + mActive[2];
            return raw.replace(trimmed, () => out);
          }
        }
        if (firstUpper === 'R') {
          if (RE_RUNNING_FOR.test(trimmed)) {
            return raw.replace(RE_RUNNING_FOR, '已运行 $1');
          }
          if (!inHdr && RE_RUNNING_CMD.test(trimmed)) {
            return raw.replace(RE_RUNNING_CMD, (_, cmd) => '正在运行 ' + cmd);
          }
        }
      } else if (firstUpper === 'D') {
        const mNamedD = trimmed.match(RE_NAMED_LIMIT_RESETS);
        if (mNamedD) {
          const pZh = NAMED_PERIOD_MAP[mNamedD[1].toLowerCase()] || mNamedD[1];
          return raw.replace(trimmed, () => pZh + '限额，' + formatCompactDurationZh(mNamedD[2]) + '后重置');
        }
        if (RE_DELETED_RECORDINGS.test(trimmed)) {
          return raw.replace(RE_DELETED_RECORDINGS, '已删除 $1 条录音 ($2)');
        }
      } else if (firstUpper === 'U') {
        const mUpd = trimmed.match(RE_UPDATED_AT);
        if (mUpd) {
          return raw.replace(trimmed, () => formatUpdatedAtZh(mUpd[1], mUpd[2]));
        }
      } else if (firstUpper === 'Z') {
        if (RE_ZIPPING_FOLDER.test(trimmed)) {
          return raw.replace(RE_ZIPPING_FOLDER, (_, folder) => '正在打包 ' + folder + '…');
        }
      } else if (firstUpper === 'Y') {
        const mPlan = trimmed.match(RE_YOUR_PLAN);
        if (mPlan) {
          return raw.replace(trimmed, () => '当前套餐：' + mPlan[1].trim());
        }
        const mSignInOn = trimmed.match(RE_SIGNING_IN_ON);
        if (mSignInOn) {
          return raw.replace(trimmed, () => '你正在 ' + mSignInOn[1].trim() + ' 上登录，登录状态仅保留在该设备上。');
        }
        const mUsed = trimmed.match(RE_USED_LIMIT);
        if (mUsed) {
          const degreeMap = { some: '部分', all: '全部', most: '大部分' };
          const typeMap = { weekly: '每周', '5-hour': '5 小时', 'five-hour': '5 小时', daily: '每日' };
          const degree = degreeMap[mUsed[1].toLowerCase()] || mUsed[1];
          const type = typeMap[mUsed[2].toLowerCase()] || mUsed[2];
          const timeStr = mUsed[3]
            .replace(/\.$/, '')
            .replace(/(\d+)\s*days?/gi, '$1 天')
            .replace(/(\d+)\s*hours?/gi, '$1 小时')
            .replace(/(\d+)\s*minutes?/gi, '$1 分钟')
            .replace(/(\d+)\s*seconds?/gi, '$1 秒')
            .replace(/,\s*/g, ' ')
            .trim();
          return '您已消耗' + degree + type + '额度，将在 ' + timeStr + ' 后完全重置。';
        }
        const mNotUsed = trimmed.match(RE_NOT_USED_LIMIT);
        if (mNotUsed) {
          const typeMap = { weekly: '每周', '5-hour': '5 小时', 'five-hour': '5 小时', daily: '每日' };
          const type = typeMap[mNotUsed[1].toLowerCase()] || mNotUsed[1];
          return '您尚未消耗任何' + type + '额度。';
        }
      } else if (firstUpper === 'A') {
        const mCred = trimmed.match(RE_AVAILABLE_AI_CREDITS);
        if (mCred) {
          return raw.replace(trimmed, () => '可用 AI 积分：' + mCred[1].trim());
        }
      }

      if (firstUpper === 'S' && trimmed.length < 240) {
        const mSideStart = trimmed.match(RE_STARTED_SIDE_QUOTE);
        if (mSideStart) {
          const quote = mSideStart[2].trim();
          const zh = mSideStart[1] === 'during' ? '发起于「' + quote + '」期间' : '发起自「' + quote + '」';
          return raw.replace(trimmed, () => zh);
        }
      }

      if (trimmed.length < 40 && firstUpper === 'E') {
        const mExit = trimmed.match(RE_EXITED_CODE_ONLY);
        if (mExit) {
          return raw.replace(trimmed, () => '退出码 ' + mExit[1] + (mExit[2] ? '。' : ''));
        }
      }
      if (trimmed.length < 240 && trimmed.includes('Exited with code')) {
        const mSumExit = trimmed.match(RE_SUMMARY_EXITED_CODE);
        if (mSumExit) {
          return raw.replace(trimmed, () => mSumExit[1].trim() + ' · 退出码 ' + mSumExit[2]);
        }
      }

      const lastCh = trimmed.charCodeAt(trimmed.length - 1);
      if (lastCh === 116 || lastCh === 84) {
        const mModLim = trimmed.match(RE_MODEL_NAMED_LIMIT);
        if (mModLim) {
          const pZh = NAMED_PERIOD_MAP[mModLim[2].toLowerCase()] || mModLim[2];
          return raw.replace(trimmed, () => mModLim[1].trim() + ' ' + pZh + '限额');
        }
      }

      return null;
    }

    function resolveTextGroup(node) {
      const parent = node.parentElement;
      if (!parent) return null;
      const kids = parent.childNodes;
      if (kids.length < 2 || kids.length > 8) return null;
      const origs = [];
      for (let i = 0; i < kids.length; i++) {
        const k = kids[i];
        if (k.nodeType !== 3) return null;
        const r = origTextMap.get(k);
        origs.push(r && r.lastVal === k.nodeValue ? r.origRaw : k.nodeValue || '');
      }
      const joined = origs.join('').trim();
      if (!joined || !RE_HAS_ALPHA.test(joined)) return null;
      let v = resolveContextualDictTranslation(joined, parent, lookupDict(joined));
      if (typeof v !== 'string') v = computeTextTranslation(joined, parent);
      return typeof v === 'string' ? { kids, origs, v } : null;
    }

    function translateTextNode(node) {
      const currentVal = node.nodeValue;
      const rec = origTextMap.get(node);
      if (!currentVal && !(rec && rec.group)) return;

      const grp = resolveTextGroup(node);
      if (grp) {
        for (let i = 0; i < grp.kids.length; i++) {
          const k = grp.kids[i];
          const nextVal = i === 0 ? grp.v : '';
          origTextMap.set(k, { origRaw: grp.origs[i], lastVal: nextVal, ver: PATCH_VERSION, group: true });
          if (k.nodeValue !== nextVal) k.nodeValue = nextVal;
        }
        return;
      }
      if (rec && rec.group) {
        origTextMap.delete(node);
        if (rec.lastVal === currentVal) node.nodeValue = rec.origRaw;
        if (node.nodeValue) translateTextNode(node);
        return;
      }

      if (rec && rec.lastVal === currentVal && rec.ver === PATCH_VERSION) {
        return;
      }
      const origRaw = rec && rec.lastVal === currentVal ? rec.origRaw : currentVal;

      const nextVal = computeTextTranslation(origRaw, node.parentElement);
      if (nextVal !== null) {
        origTextMap.set(node, { origRaw, lastVal: nextVal, ver: PATCH_VERSION });
        if (node.nodeValue !== nextVal) {
          node.nodeValue = nextVal;
        }
      } else if (rec && rec.lastVal === currentVal) {
        node.nodeValue = origRaw;
        origTextMap.delete(node);
      }
    }

    function translateSingleAttr(el, attrName, origDataAttr, customResolver) {
      const verProp = '__ag_attr_ver_' + attrName;
      const lastDataAttr = origDataAttr + '-last';
      const cur = el.getAttribute(attrName);
      if (!cur) {
        if (el.hasAttribute(origDataAttr)) el.removeAttribute(origDataAttr);
        if (el.hasAttribute(lastDataAttr)) el.removeAttribute(lastDataAttr);
        delete el[verProp];
        return;
      }
      const savedOrig = el.getAttribute(origDataAttr);
      const savedLast = el.getAttribute(lastDataAttr);
      if (savedOrig !== null && savedLast === cur && el[verProp] === PATCH_VERSION) {
        return;
      }
      const origVal = savedOrig !== null && savedLast === cur ? savedOrig : cur;
      const trimmed = origVal.trim();
      const shouldSkipAttr = attrName === 'aria-label' && SKIP_ARIA_LABEL_VALUES.has(trimmed);
      const nextVal = shouldSkipAttr
        ? null
        : customResolver
        ? customResolver(trimmed, el)
        : typeof DICT[trimmed] === 'string'
        ? DICT[trimmed]
        : null;
      if (typeof nextVal === 'string') {
        if (savedOrig !== origVal) el.setAttribute(origDataAttr, origVal);
        if (savedLast !== nextVal) el.setAttribute(lastDataAttr, nextVal);
        el[verProp] = PATCH_VERSION;
        if (cur !== nextVal) {
          el.setAttribute(attrName, nextVal);
        }
      } else {
        if (savedOrig !== null && savedLast === cur) {
          el.setAttribute(attrName, savedOrig);
        }
        if (savedOrig !== null) el.removeAttribute(origDataAttr);
        if (savedLast !== null) el.removeAttribute(lastDataAttr);
        el[verProp] = PATCH_VERSION;
      }
    }

    function resolveAttrTranslation(atrim, el) {
      if (typeof DICT[atrim] === 'string') return resolveContextualDictTranslation(atrim, el, DICT[atrim]);
      const actionZh = (a) => lookupDict(a.trim()) || a.trim();
      const mShortcut = atrim.match(RE_ARIA_SHORTCUT);
      if (mShortcut) {
        return (mShortcut[1] === 'Shortcut' ? '快捷键 ' : '移除快捷键 ') + mShortcut[2] + '：' + actionZh(mShortcut[3]);
      }
      const mAddShortcut = atrim.match(RE_ARIA_ADD_SHORTCUT);
      if (mAddShortcut) return '为“' + actionZh(mAddShortcut[1]) + '”添加快捷键';
      const mCrumb = atrim.match(RE_ARIA_CRUMB);
      if (mCrumb) return (mCrumb[1] === 'Project' ? '项目：' : '对话：') + mCrumb[2];
      const mCloseNamed = atrim.match(RE_ARIA_CLOSE_NAMED);
      if (mCloseNamed && !lookupDict(atrim)) return '关闭 ' + mCloseNamed[1];
      const mLines = atrim.match(RE_ARIA_LINES_DIFF);
      if (mLines) return '新增 ' + mLines[1] + ' 行，删除 ' + mLines[2] + ' 行';
      const mRules = atrim.match(RE_TITLE_RULES_TOKENS);
      if (mRules) return '项目规则：' + mRules[1] + ' tokens';
      const mModel = atrim.match(RE_ARIA_MODEL);
      if (mModel) {
        const mName = mModel[1].trim();
        if (mName === 'Select model') return '模型，当前：选择模型';
        return '模型，当前：' + mName;
      }
      const lookupPerm = (s) => {
        if (s && s.toLowerCase() === 'default') return '敏感确认 (Default)';
        return DICT[s] || DICT[s.charAt(0).toUpperCase() + s.slice(1).toLowerCase()] || s;
      };
      const mPermSel = atrim.match(RE_ARIA_PERM_SEL);
      if (mPermSel) {
        const pMode = mPermSel[1].trim();
        return '权限模式，当前：' + lookupPerm(pMode);
      }
      const mPermFixed = atrim.match(RE_ARIA_PERM_FIXED);
      if (mPermFixed) {
        const pMode = mPermFixed[1].trim();
        const rawNote = (mPermFixed[2] || mPermFixed[3] || '').trim();
        if (!rawNote) {
          return '权限模式：' + lookupPerm(pMode);
        }
        const mappedNote =
          rawNote.toLowerCase() === 'cannot be changed mid-conversation'
            ? '会话中途无法更改'
            : DICT[rawNote] || computeTextTranslation(rawNote, el) || rawNote;
        return '权限模式：' + lookupPerm(pMode) + '（' + mappedNote + '）';
      }
      const mTurn = atrim.match(RE_ARIA_TURN);
      if (mTurn) {
        if (mTurn[3] && mTurn[4]) {
          return '第 ' + mTurn[3] + ' / ' + mTurn[4] + ' 轮对话';
        }
        const roleZh = mTurn[2].toLowerCase() === 'user' ? '用户' : '智能体';
        return '第 ' + mTurn[1] + ' 轮对话（' + roleZh + '）';
      }
      const mOlder = atrim.match(RE_ARIA_LOAD_OLDER);
      if (mOlder) {
        if (mOlder[2] && mOlder[3]) {
          return '加载更早的消息（当前显示 ' + mOlder[2] + ' / ' + mOlder[3] + '）';
        }
        return '加载更早的 ' + mOlder[1] + ' 轮对话';
      }
      const mBranch = atrim.match(RE_ARIA_BRANCH);
      if (mBranch) {
        const bName = mBranch[1].trim();
        return '分支，当前：' + (bName === 'Select branch' ? '选择分支' : bName);
      }
      const mProj = atrim.match(RE_ARIA_PROJECT);
      if (mProj) {
        const pName = mProj[1].trim();
        return '选择项目，当前：' + (pName === 'No project' || pName === 'No Project' ? '未关联项目' : pName);
      }
      const mLoc = atrim.match(RE_ARIA_LOCATION);
      if (mLoc) {
        const locName = mLoc[1].trim();
        const mappedLoc =
          locName === 'This Computer' ? '本机' : locName === 'Remote' ? '远程' : DICT[locName] || locName;
        return '智能体运行位置，当前：' + mappedLoc;
      }
      const mVer = atrim.match(RE_ARIA_VERSION);
      if (mVer) {
        return '版本 ' + mVer[1].trim();
      }
      const mPlayback = atrim.match(RE_ARIA_PLAYBACK_SPEED);
      if (mPlayback) return '播放速度 ' + mPlayback[1];
      const mZoomReset = atrim.match(RE_ARIA_ZOOM_RESET);
      if (mZoomReset) return '缩放 ' + mZoomReset[1] + '%，点击重置为适应窗口';
      const mPlanSteps = atrim.match(RE_ARIA_PLAN_STEPS);
      if (mPlanSteps) return '计划，已完成 ' + mPlanSteps[1] + ' / ' + mPlanSteps[2] + ' 步';
      const mGoTurn = atrim.match(RE_ARIA_GO_TO_TURN);
      if (mGoTurn) return '跳转到第 ' + mGoTurn[1] + ' / ' + mGoTurn[2] + ' 轮对话';
      const mOpenSub = atrim.match(RE_ARIA_OPEN_SUBAGENT);
      if (mOpenSub) return '打开子代理：' + mOpenSub[1].trim();
      const mViewDiff = atrim.match(RE_ARIA_VIEW_DIFF_FOR);
      if (mViewDiff) return '查看 ' + mViewDiff[1].trim() + ' 的差异';
      const mDelCookies = atrim.match(RE_ARIA_DELETE_COOKIES_FOR);
      if (mDelCookies) return '删除 ' + mDelCookies[1].trim() + ' 的 Cookie';
      const mRevoke = atrim.match(RE_ARIA_REVOKE);
      if (mRevoke) return (mRevoke[1] ? '全部撤销：' : '撤销：') + mRevoke[2].trim();
      const mMoreAct = atrim.match(RE_ARIA_MORE_ACTIONS_FOR);
      if (mMoreAct) return mMoreAct[2].trim() + (mMoreAct[1].toLowerCase() === 'options' ? ' 的更多选项' : ' 的更多操作');
      const mResetDef = atrim.match(RE_ARIA_RESET_DEFAULT);
      if (mResetDef) return '将“' + actionZh(mResetDef[1]) + '”重置为默认';
      const mRecShortcut = atrim.match(RE_ARIA_RECORDING_SHORTCUT);
      if (mRecShortcut) return '正在录制“' + actionZh(mRecShortcut[1]) + '”的快捷键。按下按键绑定，Enter 保存，Esc 取消。';
      const mOpenUrlSys = atrim.match(RE_ARIA_OPEN_URL_SYS);
      if (mOpenUrlSys) return '在系统浏览器中打开链接：' + mOpenUrlSys[1].trim();
      const mGoPage = atrim.match(RE_ARIA_GO_TO_PAGE);
      if (mGoPage) return mGoPage[1].toLowerCase() === 'slide' ? '跳转到第 ' + mGoPage[2] + ' 张幻灯片' : '跳转到第 ' + mGoPage[2] + ' 页';
      const mDelSideQ = atrim.match(RE_ARIA_DELETE_SIDE_Q);
      if (mDelSideQ) return '删除旁路提问：' + mDelSideQ[1].trim();
      const mSwitchEnv = atrim.match(RE_ARIA_SWITCH_ENV);
      if (mSwitchEnv) return '切换环境 (' + mSwitchEnv[1].trim() + ')';
      const mDetailsFor = atrim.match(RE_ARIA_DETAILS_FOR);
      if (mDetailsFor) return mDetailsFor[1].trim() + ' 详情';
      const mShowInFolder = atrim.match(RE_ARIA_SHOW_IN_FOLDER);
      if (mShowInFolder) return '在文件夹中显示 ' + mShowInFolder[1].trim();
      const mDlProg = atrim.match(RE_ARIA_DOWNLOAD_PROGRESS);
      if (mDlProg) return mDlProg[1].trim() + ' 下载进度';
      const mRmList = atrim.match(RE_ARIA_REMOVE_FROM_LIST);
      if (mRmList) return '从列表中移除 ' + mRmList[1].trim();
      const mBoardSeq = atrim.match(RE_ARIA_GO_TO_BOARD_SEQ);
      if (mBoardSeq) return '前往看板第 #' + mBoardSeq[1] + ' 条消息';
      return computeTextTranslation(atrim, el);
    }

    function translateElementAttrs(el) {
      if (!el || el.nodeType !== 1) return;
      translateSingleAttr(el, 'title', 'data-ag-orig-title', resolveAttrTranslation);
      translateSingleAttr(el, 'placeholder', 'data-ag-orig-placeholder', resolveAttrTranslation);
      translateSingleAttr(el, 'aria-label', 'data-ag-orig-aria-label', resolveAttrTranslation);
    }

    const walkerFilter = {
      acceptNode(node) {
        if (node.nodeType === 1) {
          if (node.matches && node.matches(EXCLUDE_SELECTOR)) {
            if (node.isConnected && node.matches(TEXT_IMMUTABLE_EXCLUDE_SELECTOR)) {
              immutableTextExclCache.set(node, true);
            }
            if (
              (node.tagName === 'INPUT' || node.tagName === 'TEXTAREA') &&
              canTranslateElementAttrs(node) &&
              (node.hasAttribute('title') || node.hasAttribute('aria-label') || node.hasAttribute('placeholder'))
            ) {
              translateElementAttrs(node);
            }
            return NodeFilter.FILTER_REJECT;
          }
          if (
            node.hasAttribute &&
            (node.hasAttribute('title') || node.hasAttribute('aria-label') || node.hasAttribute('placeholder'))
          ) {
            translateElementAttrs(node);
          }
          return NodeFilter.FILTER_SKIP;
        }
        return NodeFilter.FILTER_ACCEPT;
      }
    };

    function scanAndTranslate(root = document.body) {
      if (!root) return;
      if (root.nodeType === 3) {
        if (root.parentElement && !isExcludedElement(root.parentElement)) {
          translateTextNode(root);
        }
        return;
      }
      if (root.nodeType !== 1) return;
      if (isExcludedElement(root)) {
        if (
          (root.tagName === 'INPUT' || root.tagName === 'TEXTAREA') &&
          canTranslateElementAttrs(root) &&
          (root.hasAttribute('title') || root.hasAttribute('aria-label') || root.hasAttribute('placeholder'))
        ) {
          translateElementAttrs(root);
        }
        return;
      }

      if (
        root.hasAttribute &&
        (root.hasAttribute('title') || root.hasAttribute('aria-label') || root.hasAttribute('placeholder'))
      ) {
        translateElementAttrs(root);
      }

      const walker = document.createTreeWalker(
        root,
        NodeFilter.SHOW_ELEMENT | NodeFilter.SHOW_TEXT,
        walkerFilter
      );
      while (walker.nextNode()) {
        translateTextNode(walker.currentNode);
      }
    }

    scanBodyFn = scanAndTranslate;
    if (document.body) {
      scanAndTranslate(document.body);
    }

    const dirtyRoots = new Set();
    let pending = false;
    let fallbackTimer = null;

    function flushTranslations() {
      pending = false;
      if (fallbackTimer) {
        clearTimeout(fallbackTimer);
        fallbackTimer = null;
      }
      if (dirtyRoots.size === 0) return;

      if (dirtyRoots.size > 200) {
        dirtyRoots.clear();
        try {
          scanAndTranslate(document.body);
        } catch (e) {}
        return;
      }

      const roots = Array.from(dirtyRoots);
      const rootSet = new Set(roots);
      dirtyRoots.clear();

      for (let i = 0; i < roots.length; i++) {
        const r = roots[i];
        if (!r || !r.isConnected) continue;
        let covered = false;
        for (let p = r.parentNode; p; p = p.parentNode) {
          if (rootSet.has(p)) {
            covered = true;
            break;
          }
        }
        if (!covered) {
          try {
            scanAndTranslate(r);
          } catch (e) {}
        }
      }
    }

    function scheduleFlush() {
      if (pending) return;
      pending = true;
      requestAnimationFrame(flushTranslations);
      fallbackTimer = setTimeout(flushTranslations, 80);
    }

    window.__ag_i18n_observer = new MutationObserver((mutations) => {
      const batchSafeSet = new Set();
      for (let i = 0; i < mutations.length; i++) {
        const m = mutations[i];
        if (m.type === 'characterData') {
          const t = m.target;
          if (t && t.parentElement && !isExcludedElement(t.parentElement, batchSafeSet)) {
            dirtyRoots.add(t);
          }
        } else if (m.type === 'attributes') {
          const el = m.target;
          if (el && el.nodeType === 1 && canTranslateElementAttrs(el, batchSafeSet)) {
            try {
              translateElementAttrs(el);
            } catch (e) {}
          }
        } else if (m.type === 'childList' && m.addedNodes.length > 0) {
          if (m.target && m.target.nodeType === 1 && isExcludedElement(m.target, batchSafeSet)) {
            continue;
          }
          for (let j = 0; j < m.addedNodes.length; j++) {
            const n = m.addedNodes[j];
            if (n.nodeType === 1 || n.nodeType === 3) {
              dirtyRoots.add(n);
            }
          }
        }
      }
      if (dirtyRoots.size > 0) {
        scheduleFlush();
      }
    });

    if (document.body) {
      window.__ag_i18n_observer.observe(document.body, I18N_OBS_OPTS);
    }
  } else if (document.body) {
    restoreAllTranslations(document.body);
  }
