-- Count active workers for a session.
SELECT CAST(COUNT(*) AS integer)
FROM workers
WHERE session_id = :session_id
