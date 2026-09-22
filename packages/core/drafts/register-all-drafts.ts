// Модуль с побочным эффектом: импортирует все стора черновиков, чтобы их
// registerDraftCleanup сработали до первого пути очистки.
import '../issues/stores/draft-store';
import '../issues/stores/quick-create-store';
import '../issues/stores/comment-draft-store';
import '../projects/draft-store';
import '../feedback/draft-store';
