/**
 * Фильтрация issue по статусу и приоритету — подмножество веб-версии,
 * с той же семантикой "пустой массив фильтров = показывать всё".
 * Фильтры по исполнителю/проекту/лейблу пока не реализованы.
 */
import type { Issue, IssuePriority, IssueStatus } from '@goosar/core/types';

export function filterIssues(
  issues: Issue[],
  statusFilters: IssueStatus[],
  priorityFilters: IssuePriority[],
): Issue[] {
  return issues.filter((issue) => {
    if (statusFilters.length > 0 && !statusFilters.includes(issue.status)) {
      return false;
    }
    if (priorityFilters.length > 0 && !priorityFilters.includes(issue.priority)) {
      return false;
    }
    return true;
  });
}
