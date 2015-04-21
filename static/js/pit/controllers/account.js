var AccountController = (function() {
	var user = LoginController.getUser();
	var key = LoginController.getKey();
	var logsTableBody = $("#logs-table-body");

	var my = {
		init: function() {
			$("#change-pass-form").submit(function(event) {
				var oldPass = $("#old-pass").val();
				var newPass = $("#new-pass").val();
				var repNewPass = $("#repeat-new-pass").val();

				$("#repeat-pass-incorrect").hide();
				$("#old-pass-incorrect").hide();
				$("#pass-updated").hide();

				if (newPass !== repNewPass) {
					$("#repeat-pass-incorrect").show();
				} else {
					$.ajax({
						type: 'POST',
						url: cAPIBase + '/change_pass',
						data: {
							u: LoginController.getUser(),
							k: oldPass,
							nk: newPass
						},
						success: function() {
							localStorage.setItem("key", newPass);
							key = newPass;
							$("#pass-updated").show();
						},
						error: function() {
							$("#old-pass-incorrect").show();
						},
					});
				}
				event.preventDefault();
			});

			$("#email-addr").text(user);

			$.ajax({
				type: 'POST',
				url: cAPIBase + '/account_logs',
				data: {
					u: user,
					k: key,
				},
				success: function(data) {
					var i = 1;
					$.each(data, function(type, logs) {
						$.each(logs, function(_, line) {
							var d = new Date(line.ts * 1000);

							logsTableBody.append($("\
								<tr>\
									<th scope=\"row\">" + i++ + "</th>\
									<td>" + line.type + "</td>\
									<td>" + d.toDateString() + " " + d.toTimeString() + "</td>\
									<td>" + line.ip.split(':')[0] + "</td>\
									<td>" + line.desc + "</td>\
								</tr>\
							"));
						});
					});
				},
				dataType: 'json'
			});
		}
	};

	return my;
})();
